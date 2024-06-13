// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/ice/v3"
	"github.com/runetale/runetale/backoff"
	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/runecfg"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Ice struct {
	// for server
	signalClient grpc.SignalClientImpl
	serverClient grpc.ServerClientImpl
	sigexec      *SigExecuter

	// for wonderwall
	updateEndpointFunc func(runecfg.Endpoint)
	endpoint           runecfg.Endpoint

	// for using local proxying, only when using turn connection
	localEndpointConn net.Conn
	remoteConn        *ice.Conn

	// channel to use when making a node connection
	remoteOfferCh  chan Credentials
	remoteAnswerCh chan Credentials

	// ice
	agent           *ice.Agent
	udpMux          *ice.UDPMuxDefault
	udpMuxSrflx     *ice.UniversalUDPMuxDefault
	udpMuxConn      *net.UDPConn
	udpMuxConnSrflx *net.UDPConn

	stunTurn *StunTurnConfig

	// remote node
	remoteWgPubKey string
	remoteNodeKey  string

	// local
	wgPubKey string
	wgPort   uint16
	nodeKey  string

	blackList []string

	iceMutex *sync.Mutex
	closeCh  chan struct{}

	ctx        context.Context
	cancelFunc context.CancelFunc

	failedTimeout *time.Duration

	log *log.Logger
}

func NewIce(
	signalClient grpc.SignalClientImpl,
	serverClient grpc.ServerClientImpl,
	updateEndpointFunc func(runecfg.Endpoint),

	remoteWgPubKey string,
	remoteNodeKey string,

	wgPrivateKey wgtypes.Key,
	wgPort uint16,
	wgIface string,
	presharedKey string,
	nk string,

	stunTurn *StunTurnConfig,
	blacklist []string,

	logger *log.Logger,

	closeCh chan struct{},
) *Ice {
	failedtimeout := time.Second * 5
	return &Ice{
		signalClient: signalClient,
		serverClient: serverClient,

		updateEndpointFunc: updateEndpointFunc,

		remoteOfferCh:  make(chan Credentials),
		remoteAnswerCh: make(chan Credentials),

		stunTurn: stunTurn,

		remoteWgPubKey: remoteWgPubKey,
		remoteNodeKey:  remoteNodeKey,

		wgPubKey: wgPrivateKey.PublicKey().String(),
		wgPort:   wgPort,
		nodeKey:  nk,

		blackList: blacklist,

		iceMutex: &sync.Mutex{},
		closeCh:  closeCh,

		failedTimeout: &failedtimeout,

		log: logger,
	}
}

// must be called before calling NewIce
func (i *Ice) Configure() (err error) {
	i.iceMutex.Lock()
	defer i.iceMutex.Unlock()

	// configure sigexe
	i.sigexec = NewSigExecuter(i.signalClient, i.remoteNodeKey, i.nodeKey, i.log)

	// configure ice agent
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

	// must be done asynchronously, separately from SignalOffer,
	// as it requires waiting for a connection channel from the other peers
	go i.waitingRemotePeerConnections()

	// configure iface
	// iface := iface.NewIface(i.wgIface, i.wgPrivKey.String(), i.ip, i.cidr, i.log)

	// configure wire proxy
	// wireproxy := proxy.NewWireProxy(
	// 	iface,
	// 	i.remoteWgPubKey,
	// 	i.remoteIp,
	// 	i.wgIface,
	// 	fmt.Sprintf("127.0.0.1:%d", i.wgPort),
	// 	i.preSharedKey,
	// 	i.log,
	// 	i.agent,
	// )

	// i.wireproxy = wireproxy

	return nil
}

func (i *Ice) IceConnectionHasBeenChanged(state ice.ConnectionState) {
	switch state {
	case ice.ConnectionStateNew: // ConnectionStateNew ICE agent is gathering addresses
		i.log.Logger.Infof("new connections collected, [%s]", state.String())
	case ice.ConnectionStateChecking: // ConnectionStateNew ICE agent is gathering addresses
		i.log.Logger.Infof("checking agent state, [%s]", state.String())
	case ice.ConnectionStateConnected: // ConnectionStateConnected ICE agent has a pairing, but is still checking other pairs
		i.log.Logger.Infof("agent [%s]", state.String())
	case ice.ConnectionStateCompleted: // ConnectionStateConnected ICE agent has a pairing, but is still checking other pairs
		err := i.signalClient.Connected()
		if err != nil {
			i.log.Logger.Errorf("the agent connection was successful but I received an error in the function that updates the status to connect, [%s]", state.String())
		}
		i.log.Logger.Infof("successfully connected to agent, [%s]", state.String())
	case ice.ConnectionStateFailed: // ConnectionStateFailed ICE agent never could successfully connect
		err := i.signalClient.DisConnected()
		if err != nil {
			i.log.Logger.Errorf("agent connection failed, but failed to set the connection state to disconnect, [%s]", state.String())
		}
	case ice.ConnectionStateDisconnected: // ConnectionStateDisconnected ICE agent connected successfully, but has entered a failed state
		err := i.signalClient.DisConnected()
		if err != nil {
			i.log.Logger.Errorf("agent connected successfully, but has entered a failed state, [%s]", state.String())
		}
	case ice.ConnectionStateClosed: // ConnectionStateClosed ICE agent has finished and is no longer handling requests
		i.log.Logger.Infof("agent has finished and is no longer handling requests, [%s]", state.String())
	}
}

func (i *Ice) IceSelectedHasCandidatePairChanged(local ice.Candidate, remote ice.Candidate) {
	i.log.Logger.Infof("[CANDIDATE COMPLETED] agent candidates were found, local:[%s] <-> remote:[%s]", local.Address(), remote.Address())
}

func (i *Ice) GetRemoteNodeKey() string {
	return i.remoteNodeKey
}

func (i *Ice) GetRemoteWgPubKey() string {
	return i.remoteWgPubKey
}

func (i *Ice) GetLocalNodeKey() string {
	return i.nodeKey
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
	i.log.Logger.Debugf("starting close ice agent process")

	i.iceMutex.Lock()
	defer i.iceMutex.Unlock()

	err := i.udpMux.Close()
	if err != nil {
		i.log.Logger.Debugf("failed to close udp mux")
		return err
	}

	err = i.udpMuxSrflx.Close()
	if err != nil {
		i.log.Logger.Debugf("failed to close udp mux srflx")
		return err
	}

	err = i.udpMuxConn.Close()
	if err != nil {
		i.log.Logger.Debugf("failed to close udp mux conn")
		return err
	}

	err = i.udpMuxConnSrflx.Close()
	if err != nil {
		i.log.Logger.Debugf("failed to close udp mux conn srlfx")
		return err
	}

	err = i.agent.Close()
	if err != nil {
		i.log.Logger.Debugf("failed to close udp mux conn srlfx")
		return err
	}

	i.signalClient.DisConnected()

	i.log.Logger.Debugf("completed clean ice agent process")

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
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3)
	operation := func() error {
		err := i.signalOffer()
		if err != nil {
			i.log.Logger.Debugf("retrying signal offer")
			return err
		}
		return nil
	}

	if err := backoff.Retry(operation, b); err != nil {
		return err
	}

	return nil
}

func (i *Ice) Cleanup() error {
	i.cancelFunc()

	err := i.closeIce()
	if err != nil {
		return err
	}

	err = i.localEndpointConn.Close()
	if err != nil {
		return err
	}

	return nil
}

func (i *Ice) closeIce() error {
	err := i.closeIceAgent()
	if err != nil {
		return err
	}

	return nil
}

// when the offer and answer come in, gather the agent's candidates and collect the process.
// then if there are no errors, go establish a connection
func (i *Ice) waitingRemotePeerConnections() error {
	var credentials Credentials
	for {
		select {
		case credentials = <-i.remoteAnswerCh:
			i.log.Logger.Infof("receive credentials from [%s]", i.remoteNodeKey)
		case credentials = <-i.remoteOfferCh:
			i.log.Logger.Infof("receive offer from [%s]", i.remoteNodeKey)
			err := i.signalAnswer()
			if err != nil {
				i.log.Logger.Errorf("failed to signal answer, %s", err.Error())
				return err
			}
			return nil
		}

		err := i.agent.GatherCandidates()
		if err != nil {
			i.log.Logger.Errorf("failed to gather candidates, %s", err.Error())
			return err
		}

		// get remoteconn
		var remoteConn *ice.Conn
		ctx := context.Background()
		if i.wgPubKey < i.remoteWgPubKey {
			remoteConn, err = i.agent.Dial(ctx, credentials.UserName, credentials.Pwd)
			if err != nil {
				i.log.Logger.Errorf("failed to dial agent")
				return err
			}
			i.log.Logger.Infof("completed dial agent")
		} else {
			remoteConn, err = i.agent.Accept(ctx, credentials.UserName, credentials.Pwd)
			if err != nil {
				i.log.Logger.Errorf("failed to accept agent")
				return err
			}
			i.log.Logger.Infof("completed accept agent")
		}

		// get relaytype
		var endpointType runecfg.EndpointType
		var endpoint *net.UDPAddr
		pair, err := i.agent.GetSelectedCandidatePair()
		if err != nil {
			return err
		}

		// turnでrelayしないときはendpointにローカルのアドレスを指定する
		// ここで設定されたendpointはReconfigDeviceで設定される
		// Reconfigでproxyをstartするために必要
		if isRelayCandidate(pair.Local) {
			endpointType = runecfg.EndpointLocal
			localConn, err := net.Dial("udp", fmt.Sprintf(":%d", i.wgPort))
			if err != nil {
				return err
			}
			endpoint, err = net.ResolveUDPAddr(localConn.LocalAddr().Network(), localConn.LocalAddr().String())
			if err != nil {
				return err
			}

			// set local endpoint conn
			i.setLocalEndpointConn(endpoint)
		} else {
			endpointType = runecfg.EndpointSTUN
			endpoint, err = net.ResolveUDPAddr("udp", remoteConn.RemoteAddr().String())
			if err != nil {
				return err
			}
			endpoint.Port = int(i.wgPort)
		}

		ep := runecfg.Endpoint{
			Addr: endpoint.AddrPort(),
			Type: endpointType,
		}

		// update endpoint
		i.setEndpoint(ep)
		i.updateEndpointFunc(ep)

		// set remoteconn
		i.setRemoteConn(remoteConn)

		_, err = i.serverClient.Connect(i.nodeKey)
		if err != nil {
			return err
		}
	}
}

func isRelayCandidate(candidate ice.Candidate) bool {
	return candidate.Type() == ice.CandidateTypeRelay
}

func (i *Ice) setEndpoint(ep runecfg.Endpoint) {
	i.endpoint = ep
}

func (i *Ice) setRemoteConn(remoteConn *ice.Conn) {
	i.remoteConn = remoteConn
}

func (i *Ice) setLocalEndpointConn(localEndpoint *net.UDPAddr) error {
	ep, err := net.DialUDP("udp", nil, localEndpoint)
	if err != nil {
		return err
	}
	i.localEndpointConn = ep
	return nil
}

func (i *Ice) signalAnswer() error {
	i.iceMutex.Lock()
	defer i.iceMutex.Unlock()

	uname, pwd, err := i.getLocalUserIceAgentCredentials()
	if err != nil {
		return err
	}

	err = i.sigexec.Answer(uname, pwd)
	if err != nil {
		return err
	}

	i.log.Logger.Infof(fmt.Sprintf("send answer to [%s]", i.remoteNodeKey))

	return nil
}

func (i *Ice) signalOffer() error {
	i.iceMutex.Lock()
	defer i.iceMutex.Unlock()

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
		i.log.Logger.Infof("send offer to [%s]", remotemk)
	default:
		i.log.Logger.Infof("%s agent waitForSignalingProcess does not seem to have been started", remotemk)
	}
}

func (i *Ice) SendRemoteAnswerCh(remotemk, uname, pwd string) {
	select {
	case i.remoteAnswerCh <- *NewCredentials(uname, pwd):
		i.log.Logger.Infof("send answer to [%s]", i.remoteNodeKey)
	default:
		i.log.Logger.Infof("answer skipping message to %s", remotemk)
	}
}

func (i *Ice) SendRemoteCandidate(candidate ice.Candidate) {
	go func() {
		i.iceMutex.Lock()
		defer i.iceMutex.Unlock()

		if i.agent == nil {
			i.log.Logger.Debugf("agent is nil")
			return
		}

		err := i.agent.AddRemoteCandidate(candidate)
		if err != nil {
			i.log.Logger.Errorf("cannot add remote candidate")
			return
		}

		i.log.Logger.Infof("send candidate to [%s]", i.remoteNodeKey)
	}()
}

// ここから
// endpointTypeがrunecfg.EndpointLocalの場合はiceでproxyをstartさせる
// EndPointSTUNの場合は何もしない？
func (i *Ice) Reconfig(why string) {
	i.iceMutex.Lock()
	defer i.iceMutex.Unlock()

	// todo (snt)
	// endpointをもう一度確認する処理を入れる

	if i.endpoint.Type.IsLocal() {
		go i.localToRemoteProxy()
		go i.remoteToLocalProxy()
	} else {
		i.log.Logger.Infof("%s is connected via %s connection", i.endpoint.Addr.String(), i.endpoint.Type.String())
	}
}

func (i *Ice) localToRemoteProxy() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-i.ctx.Done():
			return
		default:
			n, err := i.localEndpointConn.Read(buf)
			if err != nil {
				continue
			}

			_, err = i.remoteConn.Write(buf[:n])
			if err != nil {
				continue
			}
		}
	}
}

func (i *Ice) remoteToLocalProxy() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-i.ctx.Done():
			i.log.Logger.Debugf("close the local proxy")
			return
		default:
			n, err := i.remoteConn.Read(buf)
			if err != nil {
				i.log.Logger.Debugf("remoteConn cannot read localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			_, err = i.localEndpointConn.Write(buf[:n])
			if err != nil {
				i.log.Logger.Debugf("localConn cannot write localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}
		}
	}
}