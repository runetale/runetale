// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package controlplane

// this package is responsible for communication with the signal server
// it also has the structure of ice of the remote peer as a map key with the machine key of the remote peer
// when the communication with the signal server is performed and operations are performed on the peer, they will basically be performed here.
//

import (
	"strings"
	"sync"

	"github.com/pion/ice/v2"
	"github.com/runetale/client-go/runetale/runetale/v1/machine"
	"github.com/runetale/client-go/runetale/runetale/v1/negotiation"
	"github.com/runetale/runetale/backoff"
	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/rcn/rcnsock"
	"github.com/runetale/runetale/rcn/webrtc"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/wg"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type ControlPlane struct {
	signalClient grpc.SignalClientImpl
	serverClient grpc.ServerClientImpl

	sock *rcnsock.RcnSock

	peerConns map[string]*webrtc.Ice //  with ice structure per clientmachinekey
	mk        string
	conf      *conf.Conf
	stconf    *webrtc.StunTurnConfig

	mu *sync.Mutex

	ch                  chan struct{}
	waitForRemoteConnCh chan *webrtc.Ice

	runelog *runelog.Runelog
}

func NewControlPlane(
	signalClient grpc.SignalClientImpl,
	serverClient grpc.ServerClientImpl,
	sock *rcnsock.RcnSock,
	mk string,
	conf *conf.Conf,
	ch chan struct{},
	runelog *runelog.Runelog,
) *ControlPlane {
	return &ControlPlane{
		signalClient: signalClient,
		serverClient: serverClient,

		sock: sock,

		peerConns: make(map[string]*webrtc.Ice),
		mk:        mk,
		conf:      conf,

		mu:                  &sync.Mutex{},
		ch:                  ch,
		waitForRemoteConnCh: make(chan *webrtc.Ice),

		runelog: runelog,
	}
}

func (c *ControlPlane) parseStun(url, uname, pw string) (*ice.URL, error) {
	stun, err := ice.ParseURL(url)
	if err != nil {
		return nil, err
	}

	stun.Username = uname
	stun.Password = pw
	return stun, err
}

func (c *ControlPlane) parseTurn(url, uname, pw string) (*ice.URL, error) {
	turn, err := ice.ParseURL(url)
	if err != nil {
		return nil, err
	}
	turn.Username = uname
	turn.Password = pw

	return turn, err
}

// set stun turn url to use webrtc
// (shinta) be sure to call this function before using the ConnectSignalServer
func (c *ControlPlane) ConfigureStunTurnConf() error {
	conf, err := c.signalClient.GetStunTurnConfig()
	if err != nil {
		// TOOD: (shinta) retry
		return err
	}

	stun, err := c.parseStun(
		conf.RtcConfig.StunHost.Url,
		conf.RtcConfig.TurnHost.Username,
		conf.RtcConfig.TurnHost.Password,
	)
	if err != nil {
		return err
	}

	turn, err := c.parseTurn(
		conf.RtcConfig.TurnHost.Url,
		conf.RtcConfig.TurnHost.Username,
		conf.RtcConfig.TurnHost.Password,
	)
	if err != nil {
		return err
	}

	stcof := webrtc.NewStunTurnConfig(stun, turn)

	c.stconf = stcof

	return nil
}

func (c *ControlPlane) receiveSignalRequest(
	remotemk string,
	msgType negotiation.NegotiationType,
	peer *webrtc.Ice,
	uname string,
	pwd string,
	candidate string,
) error {
	switch msgType {
	case negotiation.NegotiationType_ANSWER:
		peer.SendRemoteAnswerCh(remotemk, uname, pwd)
	case negotiation.NegotiationType_OFFER:
		peer.SendRemoteOfferCh(remotemk, uname, pwd)
	case negotiation.NegotiationType_CANDIDATE:
		candidate, err := ice.UnmarshalCandidate(candidate)
		if err != nil {
			c.runelog.Logger.Errorf("can not unmarshal candidate => [%s]", candidate)
			return err
		}
		peer.SendRemoteCandidate(candidate)
	}

	return nil
}

func (c *ControlPlane) ConnectSignalServer() {
	go func() {
		b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
		operation := func() error {
			err := c.signalClient.Connect(c.mk, func(res *negotiation.NegotiationRequest) error {
				c.mu.Lock()
				defer c.mu.Unlock()

				err := c.receiveSignalRequest(
					res.GetDstPeerMachineKey(),
					res.GetType(),
					c.peerConns[res.GetDstPeerMachineKey()],
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
			close(c.ch)
			return
		}
	}()

	c.signalClient.WaitStartConnect()

	err := c.initialOfferToRemotePeer()
	if err != nil {
		close(c.ch)
		return
	}
}

func (c *ControlPlane) initialOfferToRemotePeer() error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		res, err := c.serverClient.SyncRemoteMachinesConfig(c.mk, c.conf.Spec.WgPrivateKey)
		if err != nil {
			return err
		}

		if res.GetRemotePeers() == nil {
			return nil
		}

		for _, rp := range res.GetRemotePeers() {
			i, err := c.newIce(rp, res.Ip, res.Cidr)
			if err != nil {
				return err
			}

			c.peerConns[rp.RemoteClientMachineKey] = i
			c.waitForRemoteConnCh <- i
		}

		return nil
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}
	return nil
}

// keep the latest state of Peers received from the server
func (c *ControlPlane) syncRemotePeerConfig(remotePeers []*machine.RemotePeer) error {
	remotePeerMap := make(map[string]struct{})
	for _, p := range remotePeers {
		remotePeerMap[p.GetRemoteClientMachineKey()] = struct{}{}
	}

	unnecessary := []string{}
	for p := range c.peerConns {
		if _, ok := remotePeerMap[p]; !ok {
			unnecessary = append(unnecessary, p)
		}
	}

	if len(unnecessary) == 0 {
		return nil
	}

	for _, p := range unnecessary {
		conn, exists := c.peerConns[p]
		if exists {
			delete(c.peerConns, p)
			conn.Cleanup()
		}
		c.runelog.Logger.Debugf("there are no peers, even though there should be")
	}

	c.runelog.Logger.Debugf("completed peersConn delete in signal control plane => %v", unnecessary)
	return nil
}

func (c *ControlPlane) newIce(peer *machine.RemotePeer, myip, mycidr string) (*webrtc.Ice, error) {
	k, err := wgtypes.ParseKey(c.conf.Spec.WgPrivateKey)
	if err != nil {
		return nil, err
	}

	var pk string
	if c.conf.Spec.PreSharedKey != "" {
		k, err := wgtypes.ParseKey(c.conf.Spec.PreSharedKey)
		if err != nil {
			return nil, err
		}
		pk = k.String()
	}

	remoteip := strings.Join(peer.GetAllowedIPs(), ",")
	i := webrtc.NewIce(
		c.signalClient,
		c.serverClient,
		c.sock,
		peer.RemoteWgPubKey,
		remoteip,
		peer.GetRemoteClientMachineKey(),
		myip,
		mycidr,
		k,
		wg.WgPort,
		c.conf.Spec.TunName,
		pk,
		c.mk,
		c.stconf,
		c.conf.Spec.BlackList,
		c.runelog,
		c.ch,
	)

	return i, nil
}

func (c *ControlPlane) isExistPeer(remoteMachineKey string) bool {
	_, exist := c.peerConns[remoteMachineKey]
	return exist
}

func (c *ControlPlane) WaitForRemoteConn() {
	for {
		select {
		case ice := <-c.waitForRemoteConnCh:
			if !c.signalClient.IsReady() || !c.isExistPeer(ice.GetRemoteMachineKey()) {
				c.runelog.Logger.Errorf("signal client is not available, execute loop. applicable remote peer => [%s]", ice.GetRemoteMachineKey())
				continue
			}

			c.runelog.Logger.Debugf("starting gathering process for remote machine => [%s]", ice.GetRemoteMachineKey())

			err := ice.Setup()
			if err != nil {
				c.runelog.Logger.Errorf("failed to configure gathering process for [%s]", ice.GetRemoteMachineKey())
				continue
			}

			err = ice.StartGatheringProcess()
			if err != nil {
				c.runelog.Logger.Errorf("failed to start gathering process for [%s]", ice.GetRemoteMachineKey())
				continue
			}
		}
	}
}

// maintain flexible connections by updating remote machines
// information on a regular basis, rather than only when other Machines join
func (c *ControlPlane) SyncRemoteMachine() error {
	for {
		res, err := c.serverClient.SyncRemoteMachinesConfig(c.mk, c.conf.Spec.WgPrivateKey)
		if err != nil {
			return err
		}

		if res.GetRemotePeers() != nil {
			err := c.syncRemotePeerConfig(res.GetRemotePeers())
			if err != nil {
				c.runelog.Logger.Errorf("failed to sync remote peer config")
				return err
			}
		}
	}
}

func (c *ControlPlane) Close() error {
	for mk, ice := range c.peerConns {
		if ice == nil {
			continue
		}

		err := ice.Cleanup()
		if err != nil {
			return err
		}

		c.runelog.Logger.Debugf("close the %s", mk)
	}

	c.runelog.Logger.Debugf("finished in closing the control plane")

	return nil
}

func (c *ControlPlane) ConnectSock(ip, cidr string) {
	go func() {
		err := c.sock.Connect(c.signalClient, ip, cidr)
		if err != nil {
			c.runelog.Logger.Errorf("failed connect rcn sock, %s", err.Error())
		}
		c.runelog.Logger.Debugf("rcn sock connect has been disconnected")
	}()
}
