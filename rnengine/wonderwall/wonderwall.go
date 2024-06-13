// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

// wonderwallはp2p接続をするためにUDP Hole Punchingを行う。
// 主にはwireguard wazeviceのendpointを設定する
// proxyもしているかもしれない

package wonderwall

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/pion/ice/v3"
	"github.com/pion/stun/v2"
	"github.com/runetale/client-go/runetale/runetale/v1/negotiation"
	"github.com/runetale/client-go/runetale/runetale/v1/node"
	"github.com/runetale/runetale/backoff"
	"github.com/runetale/runetale/client/grpc"
	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/rnengine/wgconfig"
	"github.com/runetale/runetale/rnengine/wonderwall/webrtc"
	"github.com/runetale/runetale/runecfg"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Conn struct {
	serverClient grpc.ServerClientImpl
	signalClient grpc.SignalClientImpl

	clientConfig *conf.ClientConfig
	stconf       *webrtc.StunTurnConfig
	//  with ice structure per ice connection
	peerConns map[string]*webrtc.Ice

	updateEndpointFunc func(runecfg.Endpoint)

	nodeKey string
	wgPort  uint16

	mu *sync.Mutex

	waitForRemoteConnCh chan *webrtc.Ice

	closeCh chan struct{}

	log *log.Logger

	debug bool
}

func NewConn(
	signalClient grpc_client.SignalClientImpl,
	serverClient grpc_client.ServerClientImpl,
	cc *conf.ClientConfig,
	nodeKey string,
	wgPort uint16,
	log *log.Logger,
	debug bool,
) *Conn {
	return &Conn{
		signalClient:        signalClient,
		serverClient:        serverClient,
		clientConfig:        cc,
		peerConns:           make(map[string]*webrtc.Ice),
		nodeKey:             nodeKey,
		wgPort:              wgPort,
		mu:                  &sync.Mutex{},
		waitForRemoteConnCh: make(chan *webrtc.Ice),
		closeCh:             make(chan struct{}),
		log:                 log,
		debug:               debug,
	}
}

func (w *Conn) WaitForRemoteConn() {
	for {
		select {
		case ice := <-w.waitForRemoteConnCh:
			if !w.signalClient.IsReady() {
				w.log.Logger.Errorf("signal client is not available, execute loop. applicable remote node => [%s]", ice.GetRemoteNodeKey())
				continue
			}

			w.log.Logger.Debugf("starting gathering process for remote node => [%s]", ice.GetRemoteNodeKey())

			err := ice.Configure()
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

func (w *Conn) Start() {
	err := w.ConfigureStunTurnConf()
	if err != nil {
		w.log.Logger.Errorf("failed to set up relay server, %s", err.Error())
	}

	err = w.SyncRemoteNodes()
	if err != nil {
		w.log.Logger.Errorf("failed to get remote nodes, %s", err.Error())
		return
	}

	go w.ConnectSignalServer()

	go w.WaitForRemoteConn()

	err = w.Join()
	if err != nil {
		w.log.Logger.Errorf("failed to join network, %s", err.Error())
		return
	}

	w.systemMonitor()

	w.log.Logger.Debugf("starting wonderwall")
}

func (w *Conn) Join() error {
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

func (w *Conn) receiveSignalRequest(
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

func (w *Conn) offerToRemotePeer() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		for _, remoteNode := range w.peerConns {
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

func (w *Conn) SetUpdateEndpointFn(endpointFn func(endpoints runecfg.Endpoint)) {
	w.updateEndpointFunc = endpointFn
}

func (w *Conn) ConnectSignalServer() {
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

func (w *Conn) parseStun(url, uname, pw string) (*stun.URI, error) {
	s, err := stun.ParseURI(url)
	if err != nil {
		return nil, err
	}

	s.Username = uname
	s.Password = pw
	return s, err
}

func (w *Conn) parseTurn(url, uname, pw string) (*stun.URI, error) {
	turn, err := stun.ParseURI(url)
	if err != nil {
		return nil, err
	}
	turn.Username = uname
	turn.Password = pw

	return turn, err
}

func (w *Conn) ConfigureStunTurnConf() error {
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

func (w *Conn) SyncRemoteNodes() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	wgPubkey, err := w.clientConfig.GetWgPublicKey()
	if err != nil {
		return err
	}

	res, err := w.serverClient.SyncRemoteNodesConfig(w.nodeKey, wgPubkey)
	if err != nil {
		return err
	}

	if res.GetRemoteNodes() == nil {
		return nil
	}

	for _, remoteNode := range res.GetRemoteNodes() {
		i, err := w.newIce(remoteNode)
		if err != nil {
			return err
		}

		w.peerConns[remoteNode.GetRemoteNodeKey()] = i
	}

	return nil
}

func (w *Conn) newIce(node *node.Node) (*webrtc.Ice, error) {
	k, err := wgtypes.ParseKey(w.clientConfig.WgPrivateKey)
	if err != nil {
		return nil, err
	}

	var pk string
	if w.clientConfig.PreSharedKey != "" {
		k, err := wgtypes.ParseKey(w.clientConfig.PreSharedKey)
		if err != nil {
			return nil, err
		}
		pk = k.String()
	}

	i := webrtc.NewIce(
		w.signalClient,
		w.serverClient,
		w.updateEndpointFunc,
		node.GetRemoteWgPubKey(),
		node.GetRemoteNodeKey(),
		k,
		w.wgPort,
		w.clientConfig.TunName,
		pk,
		w.nodeKey,
		w.stconf,
		w.clientConfig.BlackList,
		w.log,
		w.closeCh,
	)

	return i, nil
}

func (r *Conn) systemMonitor() {
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

func (w *Conn) UpdateConfig(wgConfig *wgconfig.WgConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, peer := range wgConfig.Peers {
		for _, i := range w.peerConns {
			ke := peer.PublicKey.UntypedHexString()
			if ke == i.GetRemoteWgPubKey() {
				i.Reconfig("update-wonder-wall-config")
			}
			continue
		}
	}
	return nil
}

func (w *Conn) Close() error {
	close(w.closeCh)
	w.log.Logger.Debugf("closed complete rcn")
	return nil
}
