// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package wonderwall

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v2"
	"github.com/pion/stun"
	"github.com/runetale/client-go/runetale/runetale/v1/negotiation"
	"github.com/runetale/client-go/runetale/runetale/v1/node"
	"github.com/runetale/runetale/backoff"
	"github.com/runetale/runetale/client/grpc"
	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/rnengine/wonderwall/webrtc"
	"github.com/runetale/runetale/runecfg"
	"github.com/runetale/runetale/wg"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type config struct {
	remoteConn *ice.Conn
}

type WonderWall struct {
	serverClient grpc.ServerClientImpl
	signalClient grpc.SignalClientImpl

	spec      *conf.Spec
	stconf    *webrtc.StunTurnConfig
	peerConns map[string]*webrtc.Ice //  with ice structure per nodeKey

	updateEndpointFunc func(runecfg.Endpoint)

	nodeKey string

	mu *sync.Mutex

	waitForRemoteConnCh chan *webrtc.Ice

	closeCh chan struct{}

	log *log.Logger

	debug bool
}

func NewWonderwall(
	signalClient grpc_client.SignalClientImpl,
	serverClient grpc_client.ServerClientImpl,
	spec *conf.Spec,
	nodeKey string,
	log *log.Logger,
	debug bool,
) *WonderWall {
	return &WonderWall{
		signalClient:        signalClient,
		serverClient:        serverClient,
		spec:                spec,
		nodeKey:             nodeKey,
		mu:                  &sync.Mutex{},
		waitForRemoteConnCh: make(chan *webrtc.Ice),
		closeCh:             make(chan struct{}),
		log:                 log,
		debug:               debug,
	}
}

func (w *WonderWall) WaitForRemoteConn() {
	for {
		select {
		case ice := <-w.waitForRemoteConnCh:
			if !w.signalClient.IsReady() {
				w.log.Logger.Errorf("signal client is not available, execute loop. applicable remote node => [%s]", ice.GetRemoteNodeKey())
				continue
			}

			w.log.Logger.Debugf("starting gathering process for remote node => [%s]", ice.GetRemoteNodeKey())

			err := ice.Setup()
			if err != nil {
				w.log.Logger.Errorf("failed to configure gathering process for [%s]", ice.GetRemoteNodeKey())
				continue
			}

			err = ice.StartGatheringProcess()
			if err != nil {
				w.log.Logger.Errorf("failed to start gathering process for [%s]", ice.GetRemoteNodeKey())
				continue
			}
		}
	}
}

func (w *WonderWall) Start() {
	go w.ConnectSignalServer()

	go w.WaitForRemoteConn()

	err := w.ConfigureStunTurnConf()
	if err != nil {
		w.log.Logger.Errorf("failed to set up relay server, %s", err.Error())
	}

	err = w.SyncRemoteNodes()
	if err != nil {
		w.log.Logger.Errorf("failed to get remote nodes, %s", err.Error())
		return
	}

	err = w.Join()
	if err != nil {
		w.log.Logger.Errorf("failed to join network, %s", err.Error())
		return
	}

	w.systemMonitor()

	w.log.Logger.Debugf("starting wonderwall")
}

func (w *WonderWall) Join() error {
	go func() {
		b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
		operation := func() error {
			for remoteNodeKey := range w.peerConns {
				err := w.signalClient.Join(remoteNodeKey, w.nodeKey)
				if err != nil {
					return err
				}
			}
			return nil
		}
		if err := backoff.Retry(operation, b); err != nil {
			close(w.closeCh)
			return
		}
	}()
	return nil
}

func (w *WonderWall) receiveSignalRequest(
	remotenk string,
	msgType negotiation.NegotiationType,
	dstNode *webrtc.Ice,
	uname string,
	pwd string,
	candidate string,
) error {
	switch msgType {
	case negotiation.NegotiationType_ANSWER:
		dstNode.SendRemoteAnswerCh(remotenk, uname, pwd)
	case negotiation.NegotiationType_OFFER:
		dstNode.SendRemoteOfferCh(remotenk, uname, pwd)
	case negotiation.NegotiationType_CANDIDATE:
		candidate, err := ice.UnmarshalCandidate(candidate)
		if err != nil {
			w.log.Logger.Errorf("can not unmarshal candidate => [%s]", candidate)
			return err
		}
		dstNode.SendRemoteCandidate(candidate)
	case negotiation.NegotiationType_JOIN:
		err := w.offerToRemotePeer()
		if err != nil {
			w.log.Logger.Errorf("failed to sync remote nodes")
			return err
		}
	}

	return nil
}

func (w *WonderWall) offerToRemotePeer() error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		res, err := w.serverClient.SyncRemoteNodesConfig(w.nodeKey, w.spec.WgPrivateKey)
		if err != nil {
			return err
		}

		if res.GetRemoteNodes() == nil {
			return nil
		}

		err = w.resyncRemoteNode(res.GetRemoteNodes())
		if err != nil {
			return err
		}

		for _, remoteNode := range res.GetRemoteNodes() {
			i := w.peerConns[remoteNode.GetRemoteNodeKey()]
			if i == nil {
				return errors.New("not found peerConns")
			}
			w.waitForRemoteConnCh <- i
		}

		return nil
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}
	return nil
}

func (w *WonderWall) SetUpdateEndpointFn(endpointFn func(endpoints runecfg.Endpoint)) {
	w.updateEndpointFunc = endpointFn
}

func (w *WonderWall) ConnectSignalServer() {
	go func() {
		b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
		operation := func() error {
			err := w.signalClient.Connect(w.nodeKey, func(res *negotiation.NegotiationRequest) error {
				w.mu.Lock()
				defer w.mu.Unlock()

				err := w.receiveSignalRequest(
					res.GetDstNodeKey(),
					res.GetType(),
					w.peerConns[res.GetDstNodeKey()],
					res.GetUFlag(),
					res.GetPwd(),
					res.GetCandidate(),
				)

				if err != nil {
					return err
				}

				return nil
			})
			if err != nil {
				return err
			}

			return nil
		}

		if err := backoff.Retry(operation, b); err != nil {
			close(w.closeCh)
			return
		}
	}()

	w.signalClient.WaitStartConnect()
}

func (w *WonderWall) parseStun(url, uname, pw string) (*stun.URI, error) {
	s, err := stun.ParseURI(url)
	if err != nil {
		return nil, err
	}

	s.Username = uname
	s.Password = pw
	return s, err
}

func (w *WonderWall) parseTurn(url, uname, pw string) (*stun.URI, error) {
	turn, err := stun.ParseURI(url)
	if err != nil {
		return nil, err
	}
	turn.Username = uname
	turn.Password = pw

	return turn, err
}

func (w *WonderWall) ConfigureStunTurnConf() error {
	conf, err := w.signalClient.GetStunTurnConfig()
	if err != nil {
		// TOOD: (shinta) retry
		return err
	}

	stun, err := w.parseStun(
		conf.RtcConfig.StunHost.Url,
		conf.RtcConfig.TurnHost.Username,
		conf.RtcConfig.TurnHost.Password,
	)
	if err != nil {
		return err
	}

	turn, err := w.parseTurn(
		conf.RtcConfig.TurnHost.Url,
		conf.RtcConfig.TurnHost.Username,
		conf.RtcConfig.TurnHost.Password,
	)
	if err != nil {
		return err
	}

	stcof := webrtc.NewStunTurnConfig(stun, turn)

	w.stconf = stcof

	return nil
}

func (w *WonderWall) SyncRemoteNodes() error {
	res, err := w.serverClient.SyncRemoteNodesConfig(w.nodeKey, w.spec.WgPrivateKey)
	if err != nil {
		return err
	}

	if res.GetRemoteNodes() == nil {
		return nil
	}

	for _, remoteNode := range res.GetRemoteNodes() {
		i, err := w.newIce(remoteNode, res.Ip, res.Cidr)
		if err != nil {
			return err
		}

		w.peerConns[remoteNode.GetRemoteNodeKey()] = i
	}

	return nil
}

func (w *WonderWall) newIce(node *node.Node, myip, mycidr string) (*webrtc.Ice, error) {
	k, err := wgtypes.ParseKey(w.spec.WgPrivateKey)
	if err != nil {
		return nil, err
	}

	var pk string
	if w.spec.PreSharedKey != "" {
		k, err := wgtypes.ParseKey(w.spec.PreSharedKey)
		if err != nil {
			return nil, err
		}
		pk = k.String()
	}

	remoteip := strings.Join(node.GetAllowedIPs(), ",")
	i := webrtc.NewIce(
		w.signalClient,
		w.serverClient,
		w.updateEndpointFunc,
		node.GetRemoteWgPubKey(),
		remoteip,
		node.GetRemoteNodeKey(),
		myip,
		mycidr,
		k,
		wg.WgPort,
		w.spec.TunName,
		pk,
		w.nodeKey,
		w.stconf,
		w.spec.BlackList,
		w.log,
		w.closeCh,
	)

	return i, nil
}

func (w *WonderWall) resyncRemoteNode(remotePeers []*node.Node) error {
	remotePeerMap := make(map[string]struct{})
	for _, p := range remotePeers {
		remotePeerMap[p.GetRemoteNodeKey()] = struct{}{}
	}

	unnecessary := []string{}
	for p := range w.peerConns {
		if _, ok := remotePeerMap[p]; !ok {
			unnecessary = append(unnecessary, p)
		}
	}

	if len(unnecessary) == 0 {
		return nil
	}

	for _, p := range unnecessary {
		conn, exists := w.peerConns[p]
		if exists {
			delete(w.peerConns, p)
			conn.Cleanup()
		}
	}

	w.log.Logger.Debugf("completed nodes delete in control plane => %v", unnecessary)
	return nil
}

func (r *WonderWall) systemMonitor() {
	if r.debug {
		go func() {
			http.ListenAndServe("localhost:6060", nil)
			m := struct {
				Alloc,
				TotalAlloc,
				Sys,
				Mallocs,
				Frees,
				LiveObjects,
				PauseTotalNs uint64

				NumGC        uint32
				NumGoroutine int
			}{}

			var rtm runtime.MemStats
			for {
				select {
				case _ = <-time.NewTicker(10000 * time.Millisecond).C:
					runtime.ReadMemStats(&rtm)

					m.NumGoroutine = runtime.NumGoroutine()
					m.Alloc = rtm.Alloc
					m.TotalAlloc = rtm.TotalAlloc
					m.Sys = rtm.Sys
					m.Mallocs = rtm.Mallocs
					m.Frees = rtm.Frees
					m.LiveObjects = m.Mallocs - m.Frees
					m.PauseTotalNs = rtm.PauseTotalNs
					m.NumGC = rtm.NumGC

					b, _ := json.Marshal(m)
					r.log.Logger.Debugf(string(b))
				}
			}
		}()
	}
}

func (w *WonderWall) Stop() error {
	// err = iface.RemoveIface(r.iface.Tun, r.log)
	// if err != nil {
	// 	r.log.jkLogger.Errorf("failed to remove iface, because %s", err.Error())
	// 	return err
	// }

	close(w.closeCh)
	w.log.Logger.Debugf("closed complete rcn")
	return nil
}
