// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package proxy

import (
	"context"
	"net"

	"github.com/pion/ice/v2"
	"github.com/runetale/runetale/iface"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/wg"
)

type WireProxy struct {
	iface *iface.Iface

	// proxy config
	remoteWgPubKey string // remote node wg pub key
	remoteIp       string // remote node ip
	wgIface        string // your wg iface
	listenAddr     string // proxy addr
	preSharedKey   string // your preshared key

	remoteConn net.Conn
	localConn  net.Conn

	agent *ice.Agent

	ctx        context.Context
	cancelFunc context.CancelFunc

	runelog *runelog.Runelog
}

// TODO: (shinta) rewrite to proxy using sock5?
func NewWireProxy(
	iface *iface.Iface,
	remoteWgPubKey string,
	remoteip string,
	wgiface string,
	listenAddr string,
	presharedkey string,
	runelog *runelog.Runelog,
	agent *ice.Agent,
) *WireProxy {
	ctx, cancel := context.WithCancel(context.Background())

	return &WireProxy{
		iface: iface,

		remoteWgPubKey: remoteWgPubKey,
		remoteIp:       remoteip,

		wgIface:      wgiface,
		listenAddr:   listenAddr,
		preSharedKey: presharedkey,

		agent: agent,

		ctx:        ctx,
		cancelFunc: cancel,

		runelog: runelog,
	}
}

func (w *WireProxy) setup(remote *ice.Conn) error {
	w.remoteConn = remote
	udpConn, err := net.Dial("udp", w.listenAddr)
	if err != nil {
		return err
	}
	w.localConn = udpConn

	return nil
}

func (w *WireProxy) configureNoProxy() error {
	w.runelog.Logger.Debugf("using no proxy")

	udpAddr, err := net.ResolveUDPAddr("udp", w.remoteConn.RemoteAddr().String())
	if err != nil {
		return err
	}
	udpAddr.Port = wg.WgPort

	err = w.iface.ConfigureRemoteNodePeer(
		w.remoteWgPubKey,
		w.remoteIp,
		udpAddr,
		wg.DefaultWgKeepAlive,
		w.preSharedKey,
	)
	if err != nil {
		w.runelog.Logger.Errorf("failed to start no proxy, %s", err.Error())
		return err
	}

	return nil

}

func (w *WireProxy) configureWireProxy() error {
	w.runelog.Logger.Debugf("using wire proxy")

	udpAddr, err := net.ResolveUDPAddr(w.localConn.LocalAddr().Network(), w.localConn.LocalAddr().String())
	if err != nil {
		return err
	}

	err = w.iface.ConfigureRemoteNodePeer(
		w.remoteWgPubKey,
		w.remoteIp,
		udpAddr,
		wg.DefaultWgKeepAlive,
		w.preSharedKey,
	)
	if err != nil {
		w.runelog.Logger.Errorf("failed to start wire proxy, %s", err.Error())
		return err
	}

	return nil
}

func (w *WireProxy) Stop() error {
	w.cancelFunc()

	if w.localConn == nil {
		w.runelog.Logger.Errorf("error is unexpected, you are most likely referring to locallConn without calling the setup function")
		return nil
	}

	err := w.iface.RemoveRemotePeer(w.wgIface, w.remoteIp, w.remoteWgPubKey)
	if err != nil {
		return err
	}

	return nil
}

func shouldUseProxy(pair *ice.CandidatePair) bool {
	remoteIP := net.ParseIP(pair.Remote.Address())
	myIp := net.ParseIP(pair.Local.Address())
	remoteIsPublic := IsPublicIP(remoteIP)
	myIsPublic := IsPublicIP(myIp)

	if remoteIsPublic && pair.Remote.Type() == ice.CandidateTypeHost {
		return false
	}
	if myIsPublic && pair.Local.Type() == ice.CandidateTypeHost {
		return false
	}

	if pair.Local.Type() == ice.CandidateTypeHost && pair.Remote.Type() == ice.CandidateTypeHost {
		if !remoteIsPublic && !myIsPublic {
			return false
		}
	}

	return true
}

func IsPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return false
	}
	return true
}

func (w *WireProxy) StartProxy(remote *ice.Conn) error {
	err := w.setup(remote)
	if err != nil {
		return err
	}

	pair, err := w.agent.GetSelectedCandidatePair()
	if err != nil {
		return err
	}

	// TODO (shinta) refactor
	if shouldUseProxy(pair) {
		err = w.configureWireProxy()
		if err != nil {
			return err
		}
		w.startMon()

		return nil
	}

	err = w.configureNoProxy()
	if err != nil {
		return err
	}

	w.startMon()

	return nil
}

func (w *WireProxy) startMon() {
	w.runelog.Logger.Infof("starting mon")
	go w.monLocalToRemoteProxy()
	go w.monRemoteToLocalProxy()
}

func (w *WireProxy) monLocalToRemoteProxy() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			n, err := w.localConn.Read(buf)
			if err != nil {
				w.runelog.Logger.Errorf("localConn cannot read remoteProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			_, err = w.remoteConn.Write(buf[:n])
			if err != nil {
				w.runelog.Logger.Errorf("localConn cannot write remoteProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// TODO: gathering buffer with runemon
			// w.runelog.Logger.Debugf("remoteConn read remoteProxyBuffer [%s]", w.remoteProxyBuffer[:n])
		}
	}
}

func (w *WireProxy) monRemoteToLocalProxy() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-w.ctx.Done():
			w.runelog.Logger.Errorf("close the local proxy. close the remote ip here [%s], ", w.remoteIp)
			return
		default:
			n, err := w.remoteConn.Read(buf)
			if err != nil {
				w.runelog.Logger.Errorf("remoteConn cannot read localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			_, err = w.localConn.Write(buf[:n])
			if err != nil {
				w.runelog.Logger.Errorf("localConn cannot write localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// TODO: gathering buffer with runemon
			// w.runelog.Logger.Debugf("localConn read localProxyBuffer [%s]", w.localProxyBuffer[:n])
		}
	}
}
