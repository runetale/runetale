// license that can be found in the LICENSE file.

package proxy

import (
	"context"
	"fmt"
	"net"

	"github.com/pion/ice/v2"
	"github.com/runetale/runetale/iface"
	"github.com/runetale/runetale/log"
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

	log *log.Logger
}

func NewWireProxy(
	iface *iface.Iface,
	remoteWgPubKey string,
	remoteip string,
	wgiface string,
	listenAddr string,
	presharedkey string,
	log *log.Logger,
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

		log: log,
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
	w.log.Logger.Debugf("using no proxy")

	udpAddr, err := net.ResolveUDPAddr("udp", w.remoteConn.RemoteAddr().String())
	if err != nil {
		return err
	}
	udpAddr.Port = wg.WgPort

	// ここがuapiを叩いて変更するようにする？
	// err = w.iface.ConfigureRemoteNodePeer(
	// 	w.remoteWgPubKey,
	// 	w.remoteIp,
	// 	udpAddr,
	// 	wg.DefaultWgKeepAlive,
	// 	w.preSharedKey,
	// )
	// if err != nil {
	// 	w.log.Logger.Errorf("failed to start no proxy, %s", err.Error())
	// 	return err
	// }

	return nil

}

func (w *WireProxy) configureWireProxy() error {
	w.log.Logger.Debugf("using wire proxy")

	udpAddr, err := net.ResolveUDPAddr(w.localConn.LocalAddr().Network(), w.localConn.LocalAddr().String())
	if err != nil {
		return err
	}
	fmt.Println(udpAddr)

	// ここがuapiを叩いて変更するようにする？
	// err = w.iface.ConfigureRemoteNodePeer(
	// 	w.remoteWgPubKey,
	// 	w.remoteIp,
	// 	udpAddr,
	// 	wg.DefaultWgKeepAlive,
	// 	w.preSharedKey,
	// )
	// if err != nil {
	// 	w.log.Logger.Errorf("failed to start wire proxy, %s", err.Error())
	// 	return err
	// }

	return nil
}

func (w *WireProxy) Stop() error {
	w.cancelFunc()

	if w.localConn == nil {
		w.log.Logger.Errorf("error is unexpected, you are most likely referring to locallConn without calling the setup function")
		return nil
	}

	// // ここがuapiを叩いて変更するようにする？
	// err := w.iface.RemoveRemotePeer(w.wgIface, w.remoteIp, w.remoteWgPubKey)
	// if err != nil {
	// 	return err
	// }

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
	var err error

	w.remoteConn = remote
	w.localConn, err = net.Dial("udp", w.listenAddr)
	if err != nil {
		return err
	}

	pair, err := w.agent.GetSelectedCandidatePair()
	if err != nil {
		return err
	}

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
	w.log.Logger.Infof("starting mon")
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
			// ここでACLも可能
			n, err := w.localConn.Read(buf)
			if err != nil {
				w.log.Logger.Debugf("localConn cannot read remoteProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// ここでACLも可能
			_, err = w.remoteConn.Write(buf[:n])
			if err != nil {
				w.log.Logger.Debugf("localConn cannot write remoteProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// TODO: gathering buffer with runemon
			// w.log.Logger.Debugf("remoteConn read remoteProxyBuffer [%s]", w.remoteProxyBuffer[:n])
		}
	}
}

func (w *WireProxy) monRemoteToLocalProxy() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-w.ctx.Done():
			w.log.Logger.Debugf("close the local proxy. close the remote ip => [%s], ", w.remoteIp)
			return
		default:
			// ここでACLも可能
			n, err := w.remoteConn.Read(buf)
			if err != nil {
				w.log.Logger.Debugf("remoteConn cannot read localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// ここでACLも可能
			_, err = w.localConn.Write(buf[:n])
			if err != nil {
				w.log.Logger.Debugf("localConn cannot write localProxyBuffer [%s], size is %d", string(buf), n)
				continue
			}

			// TODO: gathering buffer with runemon
			// w.log.Logger.Debugf("localConn read localProxyBuffer [%s]", w.localProxyBuffer[:n])
		}
	}
}
