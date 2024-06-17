// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package controlplane

// this package is responsible for communication with the signal server
// it also has the structure of ice of the remote node as a map key with the Node key of the remote node
// when the communication with the signal server is performed and operations are performed on the node, they will basically be performed here.
//

import (
	"fmt"
	"strings"
	"sync"

	"github.com/pion/ice/v2"
	"github.com/runetale/client-go/runetale/runetale/v1/negotiation"
	"github.com/runetale/client-go/runetale/runetale/v1/node"
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

	peerConns map[string]*webrtc.Ice //  with ice structure per nodePey
	nk        string
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
	nk string,
	conf *conf.Conf,
	ch chan struct{},
	runelog *runelog.Runelog,
) *ControlPlane {
	return &ControlPlane{
		signalClient: signalClient,
		serverClient: serverClient,

		sock: sock,

		peerConns: make(map[string]*webrtc.Ice),
		nk:        nk,
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
		fmt.Println(remotenk)
		fmt.Println(uname)
		fmt.Println(pwd)
		fmt.Println(dstNode)
		dstNode.SendRemoteOfferCh(remotenk, uname, pwd)
	case negotiation.NegotiationType_CANDIDATE:
		candidate, err := ice.UnmarshalCandidate(candidate)
		if err != nil {
			c.runelog.Logger.Errorf("can not unmarshal candidate => [%s]", candidate)
			return err
		}
		dstNode.SendRemoteCandidate(candidate)
	case negotiation.NegotiationType_JOIN:
		err := c.offerToRemotePeer()
		if err != nil {
			c.runelog.Logger.Errorf("failed to sync remote nodes")
			return err
		}
	}

	return nil
}

func (c *ControlPlane) ConnectSignalServer() {
	go func() {
		b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
		operation := func() error {
			err := c.signalClient.Connect(c.nk, func(res *negotiation.NegotiationRequest) error {
				c.mu.Lock()
				defer c.mu.Unlock()

				fmt.Println("peer conns")
				fmt.Println(c.peerConns)

				err := c.receiveSignalRequest(
					res.GetDstNodeKey(),
					res.GetType(),
					c.peerConns[res.GetDstNodeKey()],
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
}

// keep the latest state of Peers received from the server
func (c *ControlPlane) syncRemoteNode(remotePeers []*node.Node) error {
	remotePeerMap := make(map[string]struct{})
	for _, p := range remotePeers {
		remotePeerMap[p.GetRemoteNodeKey()] = struct{}{}
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

	c.runelog.Logger.Debugf("completed nodes delete in control plane => %v", unnecessary)
	return nil
}

func (c *ControlPlane) newIce(node *node.Node, myip, mycidr string) (*webrtc.Ice, error) {
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

	remoteip := strings.Join(node.GetAllowedIPs(), ",")
	i := webrtc.NewIce(
		c.signalClient,
		c.serverClient,
		c.sock,
		node.GetRemoteWgPubKey(),
		remoteip,
		node.GetRemoteNodeKey(),
		myip,
		mycidr,
		k,
		wg.WgPort,
		c.conf.Spec.TunName,
		pk,
		c.nk,
		c.stconf,
		c.conf.Spec.BlackList,
		c.runelog,
		c.ch,
	)

	return i, nil
}

func (c *ControlPlane) isExistPeer(remoteNodeKey string) bool {
	_, exist := c.peerConns[remoteNodeKey]
	return exist
}

// note: (snt)
// Set up ice for each node and wait for gathering from the remote node.
func (c *ControlPlane) WaitForRemoteConn() {
	for {
		select {
		case ice := <-c.waitForRemoteConnCh:
			if !c.signalClient.IsReady() || !c.isExistPeer(ice.GetRemoteNodeKey()) {
				c.runelog.Logger.Errorf("signal client is not available, execute loop. applicable remote node => [%s]", ice.GetRemoteNodeKey())
				continue
			}

			c.runelog.Logger.Debugf("starting gathering process for remote node => [%s]", ice.GetRemoteNodeKey())

			err := ice.Setup()
			if err != nil {
				c.runelog.Logger.Errorf("failed to configure gathering process for [%s]", ice.GetRemoteNodeKey())
				continue
			}

			err = ice.StartGatheringProcess()
			if err != nil {
				c.runelog.Logger.Errorf("failed to start gathering process for [%s]", ice.GetRemoteNodeKey())
				continue
			}
		}
	}
}

func (c *ControlPlane) Close() error {
	for nk, ice := range c.peerConns {
		if ice == nil {
			continue
		}

		err := ice.Cleanup()
		if err != nil {
			return err
		}

		c.runelog.Logger.Debugf("close the %s", nk)
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

func (c *ControlPlane) SyncRemoteNodes() error {
	res, err := c.serverClient.SyncRemoteNodesConfig(c.nk, c.conf.Spec.WgPrivateKey)
	if err != nil {
		return err
	}

	if res.GetRemoteNodes() == nil {
		return nil
	}

	for _, remoteNode := range res.GetRemoteNodes() {
		i, err := c.newIce(remoteNode, res.Ip, res.Cidr)
		if err != nil {
			return err
		}
		c.peerConns[remoteNode.GetRemoteNodeKey()] = i
	}

	return nil
}

func (c *ControlPlane) offerToRemotePeer() error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {

		res, err := c.serverClient.SyncRemoteNodesConfig(c.nk, c.conf.Spec.WgPrivateKey)
		if err != nil {
			return err
		}

		if res.GetRemoteNodes() == nil {
			return nil
		}

		err = c.syncRemoteNode(res.GetRemoteNodes())
		if err != nil {
			return err
		}

		for _, remoteNode := range res.GetRemoteNodes() {
			i, err := c.newIce(remoteNode, res.Ip, res.Cidr)
			if err != nil {
				return err
			}

			c.peerConns[remoteNode.GetRemoteNodeKey()] = i
			c.waitForRemoteConnCh <- i
		}

		return nil
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}
	return nil
}

func (c *ControlPlane) JoinRuneNetwork() error {
	go func() {
		b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
		operation := func() error {
			for remoteNodeKey := range c.peerConns {
				err := c.signalClient.Join(remoteNodeKey, c.nk)
				if err != nil {
					return err
				}
			}
			return nil
		}
		if err := backoff.Retry(operation, b); err != nil {
			close(c.ch)
			return
		}
	}()
	return nil
}
