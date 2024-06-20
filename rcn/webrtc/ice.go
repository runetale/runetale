// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

// ice and provides webrtc functionalit
// ice initializes one structure per remote Node key
//

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/ice/v2"
	"github.com/runetale/runetale/backoff"
	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/iface"
	"github.com/runetale/runetale/rcn/conn"
	"github.com/runetale/runetale/rcn/proxy"
	"github.com/runetale/runetale/rcn/rcnsock"
	"github.com/runetale/runetale/runelog"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Ice struct {
	signalClient grpc.SignalClientImpl
	serverClient grpc.ServerClientImpl

	sock *rcnsock.RcnSock

	sigexec *SigExecuter

	conn *conn.Conn

	wireproxy *proxy.WireProxy

	// channel to use when making a node connection
	remoteOfferCh     chan Credentials
	remoteAnswerCh    chan Credentials
	remoteCandidateCh chan Credentials

	agent           *ice.Agent
	udpMux          *ice.UDPMuxDefault
	udpMuxSrflx     *ice.UniversalUDPMuxDefault
	udpMuxConn      *net.UDPConn
	udpMuxConnSrflx *net.UDPConn

	stunTurn *StunTurnConfig

	remoteWgPubKey string
	remoteIp       string
	remoteNodeKey  string

	// local
	wgPubKey     string
	wgPrivKey    wgtypes.Key
	wgIface      string
	wgPort       int
	preSharedKey string

	// for iface
	ip   string
	cidr string

	nk string

	blackList []string

	mu      *sync.Mutex
	closeCh chan struct{}

	failedTimeout *time.Duration

	runelog *runelog.Runelog
}

func NewIce(
	signalClient grpc.SignalClientImpl,
	serverClient grpc.ServerClientImpl,
	sock *rcnsock.RcnSock,

	remoteWgPubKey string,
	remoteip string,
	remoteNodeKey string,

	ip string,
	cidr string,
	wgPrivateKey wgtypes.Key,
	wgPort int,
	wgIface string,
	presharedKey string,
	nk string,

	stunTurn *StunTurnConfig,
	blacklist []string,

	runelog *runelog.Runelog,

	closeCh chan struct{},
) *Ice {
	failedtimeout := time.Second * 5
	return &Ice{
		signalClient: signalClient,
		serverClient: serverClient,

		sock: sock,

		remoteOfferCh:  make(chan Credentials),
		remoteAnswerCh: make(chan Credentials),

		stunTurn: stunTurn,

		remoteWgPubKey: remoteWgPubKey,
		remoteIp:       remoteip,
		remoteNodeKey:  remoteNodeKey,

		wgPubKey:     wgPrivateKey.PublicKey().String(),
		wgPrivKey:    wgPrivateKey,
		wgIface:      wgIface,
		wgPort:       wgPort,
		preSharedKey: presharedKey,
		ip:           ip,
		cidr:         cidr,
		nk:           nk,

		blackList: blacklist,

		mu:      &sync.Mutex{},
		closeCh: closeCh,

		failedTimeout: &failedtimeout,

		runelog: runelog,
	}
}

// must be called before calling NewIce
func (i *Ice) Setup() (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// configure sigexe
	//
	i.sigexec = NewSigExecuter(i.signalClient, i.remoteNodeKey, i.nk, i.runelog)

	// configure ice agent
	// random port
	i.udpMuxConn, err = net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	i.udpMuxConnSrflx, err = net.ListenUDP("udp4", &net.UDPAddr{Port: 0})

	i.udpMux = ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: i.udpMuxConn})
	i.udpMuxSrflx = ice.NewUniversalUDPMuxDefault(ice.UniversalUDPMuxParams{UDPConn: i.udpMuxConnSrflx})

	i.agent, err = ice.NewAgent(&ice.AgentConfig{
		MulticastDNSMode: ice.MulticastDNSModeDisabled,
		NetworkTypes:     []ice.NetworkType{ice.NetworkTypeUDP4},
		Urls:             i.stunTurn.GetStunTurnsURL(),
		CandidateTypes:   []ice.CandidateType{ice.CandidateTypeHost, ice.CandidateTypeServerReflexive, ice.CandidateTypeRelay},
		FailedTimeout:    i.failedTimeout,
		InterfaceFilter:  i.getBlackListWithInterfaceFilter(),
		UDPMux:           i.udpMux,
		UDPMuxSrflx:      i.udpMuxSrflx,
	})
	if err != nil {
		return err
	}

	// configure ice candidate functions
	err = i.agent.OnCandidate(i.sigexec.Candidate)
	if err != nil {
		return err
	}

	err = i.agent.OnConnectionStateChange(i.IceConnectionHasBeenChanged)
	if err != nil {
		return err
	}

	err = i.agent.OnSelectedCandidatePairChange(i.IceSelectedHasCandidatePairChanged)
	if err != nil {
		return err
	}

	// configure iface
	iface := iface.NewIface(i.wgIface, i.wgPrivKey.String(), i.ip, i.cidr, i.runelog)

	// configure wire proxy
	wireproxy := proxy.NewWireProxy(
		iface,
		i.remoteWgPubKey,
		i.remoteIp,
		i.wgIface,
		fmt.Sprintf("127.0.0.1:%d", i.wgPort),
		i.preSharedKey,
		i.runelog,
		i.agent,
	)

	i.wireproxy = wireproxy

	return nil
}

// TODO: (shinta)
// more detailed handling is needed.
// by handling failures, we need to establish a connection path using DoubleNat? or
// Ether(call me エーテル) when a connection cannot be made.
func (i *Ice) IceConnectionHasBeenChanged(state ice.ConnectionState) {
	switch state {
	case ice.ConnectionStateNew: // ConnectionStateNew ICE agent is gathering addresses
		i.runelog.Logger.Infof("new connections collected, [%s]", state.String())
	case ice.ConnectionStateChecking: // ConnectionStateNew ICE agent is gathering addresses
		i.runelog.Logger.Infof("checking agent state, [%s]", state.String())
	case ice.ConnectionStateConnected: // ConnectionStateConnected ICE agent has a pairing, but is still checking other pairs
		i.runelog.Logger.Infof("agent [%s]", state.String())
	case ice.ConnectionStateCompleted: // ConnectionStateConnected ICE agent has a pairing, but is still checking other pairs
		err := i.signalClient.Connected()
		if err != nil {
			i.runelog.Logger.Errorf("the agent connection was successful but I received an error in the function that updates the status to connect, [%s]", state.String())
		}
		i.runelog.Logger.Infof("successfully connected to agent, [%s]", state.String())
	case ice.ConnectionStateFailed: // ConnectionStateFailed ICE agent never could successfully connect
		err := i.signalClient.DisConnected()
		if err != nil {
			i.runelog.Logger.Errorf("agent connection failed, but failed to set the connection state to disconnect, [%s]", state.String())
		}
	case ice.ConnectionStateDisconnected: // ConnectionStateDisconnected ICE agent connected successfully, but has entered a failed state
		err := i.signalClient.DisConnected()
		if err != nil {
			i.runelog.Logger.Errorf("agent connected successfully, but has entered a failed state, [%s]", state.String())
		}
	case ice.ConnectionStateClosed: // ConnectionStateClosed ICE agent has finished and is no longer handling requests
		i.runelog.Logger.Infof("agent has finished and is no longer handling requests, [%s]", state.String())
	}
}

func (i *Ice) IceSelectedHasCandidatePairChanged(local ice.Candidate, remote ice.Candidate) {
	i.runelog.Logger.Infof("[CANDIDATE COMPLETED] agent candidates were found, local:[%s] <-> remote:[%s]", local.Address(), remote.Address())
}

func (i *Ice) GetRemoteNodeKey() string {
	return i.remoteNodeKey
}

func (i *Ice) GetLocalNodeKey() string {
	return i.nk
}

func (i *Ice) getBlackListWithInterfaceFilter() func(string) bool {
	var blackListMap map[string]struct{}
	if i.blackList != nil {
		blackListMap = make(map[string]struct{})
		for _, s := range i.blackList {
			blackListMap[s] = struct{}{}
		}
	}

	return func(iFace string) bool {
		if len(blackListMap) == 0 {
			return true
		}
		_, ok := blackListMap[iFace]
		return !ok
	}
}

func (i *Ice) closeIceAgent() error {
	i.runelog.Logger.Debugf("starting close ice agent process")

	i.mu.Lock()
	defer i.mu.Unlock()

	err := i.udpMuxSrflx.Close()
	if err != nil {
		i.runelog.Logger.Debugf("failed to close udp mux srflx")
		return err
	}

	err = i.udpMuxConn.Close()
	if err != nil {
		i.runelog.Logger.Debugf("failed to close udp mux conn")
		return err
	}

	err = i.udpMuxConnSrflx.Close()
	if err != nil {
		i.runelog.Logger.Debugf("failed to close udp mux conn srlfx")
		return err
	}

	err = i.agent.Close()
	if err != nil {
		i.runelog.Logger.Debugf("failed to close ice agent")
		return err
	}

	err = i.udpMux.Close()
	if err != nil {
		i.runelog.Logger.Debugf("failed to close udp mux")
		return err
	}

	i.signalClient.DisConnected()

	i.runelog.Logger.Debugf("completed clean ice agent process")

	return nil
}

func (i *Ice) getLocalUserIceAgentCredentials() (string, string, error) {
	uname, pwd, err := i.agent.GetLocalUserCredentials()
	if err != nil {
		return "", "", err
	}

	return uname, pwd, nil
}

// asynchronously waits for a signal process from another node before sending an offer
func (i *Ice) StartGatheringProcess() error {
	// must be done asynchronously, separately from SignalOffer,
	// as it requires waiting for a connection channel from the other peers
	go i.WaitingRemotePeerConnections()

	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		err := i.signalOffer()
		if err != nil {
			i.runelog.Logger.Debugf("retrying signal offer")
			return err
		}
		return nil
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}

	return nil
}

func (i *Ice) startConn(uname, pwd string) error {
	i.conn = conn.NewConn(
		i.agent,
		uname,
		pwd,
		i.wireproxy,
		i.remoteWgPubKey,
		i.wgPubKey,
		i.runelog,
	)

	err := i.conn.Start()
	if err != nil {
		return err
	}

	return nil
}

func (i *Ice) CloseConn() error {
	if i.conn != nil {
		err := i.conn.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *Ice) Cleanup() error {
	if i.conn != nil {
		err := i.conn.Close()
		if err != nil {
			return err
		}

		err = i.CloseIce()
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *Ice) CloseIce() error {
	err := i.closeIceAgent()
	if err != nil {
		return err
	}

	return nil
}

// when the offer and answer come in, gather the agent's candidates and collect the process.
// then if there are no errors, go establish a connection
func (i *Ice) WaitingRemotePeerConnections() error {
	var credentials Credentials
	for {
		select {
		case credentials = <-i.remoteAnswerCh:
			i.runelog.Logger.Infof("receive credentials from [%s]", i.remoteNodeKey)
		case credentials = <-i.remoteOfferCh:
			i.runelog.Logger.Infof("receive offer from [%s]", i.remoteNodeKey)
			err := i.signalAnswer()
			if err != nil {
				i.runelog.Logger.Errorf("failed to signal answer, %s", err.Error())
				return err
			}
			// return nil
		}

		err := i.agent.GatherCandidates()
		if err != nil {
			i.runelog.Logger.Errorf("failed to gather candidates, %s", err.Error())
			return err
		}

		err = i.startConn(credentials.UserName, credentials.Pwd)
		if err != nil {
			i.runelog.Logger.Errorf("failed to start conn, %s", err.Error())
			return err
		}

		_, err = i.serverClient.Connect(i.nk)
		if err != nil {
			return err
		}
	}
}

func (i *Ice) signalAnswer() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	uname, pwd, err := i.getLocalUserIceAgentCredentials()
	if err != nil {
		return err
	}

	err = i.sigexec.Answer(uname, pwd)
	if err != nil {
		return err
	}

	i.runelog.Logger.Infof(fmt.Sprintf("send answer to [%s]", i.remoteNodeKey))

	return nil
}

func (i *Ice) signalOffer() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	uname, pwd, err := i.getLocalUserIceAgentCredentials()
	if err != nil {
		return err
	}

	err = i.sigexec.Offer(uname, pwd)
	if err != nil {
		return err
	}

	return nil
}

func (i *Ice) SendRemoteOfferCh(remotemk, uname, pwd string) {
	select {
	case i.remoteOfferCh <- *NewCredentials(uname, pwd):
		i.runelog.Logger.Infof("send offer to [%s]", remotemk)
	default:
		i.runelog.Logger.Infof("%s agent waitForSignalingProcess does not seem to have been started", remotemk)
	}
}

func (i *Ice) SendRemoteAnswerCh(remotemk, uname, pwd string) {
	select {
	case i.remoteAnswerCh <- *NewCredentials(uname, pwd):
		i.runelog.Logger.Infof("send answer to [%s]", i.remoteNodeKey)
	default:
		i.runelog.Logger.Infof("answer skipping message to %s", remotemk)
	}
}

func (i *Ice) SendRemoteCandidate(candidate ice.Candidate) {
	go func() {
		i.mu.Lock()
		defer i.mu.Unlock()

		if i.agent == nil {
			i.runelog.Logger.Debugf("agent is nil")
			return
		}

		err := i.agent.AddRemoteCandidate(candidate)
		if err != nil {
			i.runelog.Logger.Errorf("cannot add remote candidate")
			return
		}

		i.runelog.Logger.Infof("send candidate to [%s]", i.remoteNodeKey)
	}()
}
