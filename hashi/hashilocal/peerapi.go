package hashilocal

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync"

	"github.com/runetale/runetale/net/ipaddr"
)

var initListenConfig func(*net.ListenConfig, netip.Addr, *net.Interface) error

type peerAPIServer struct {
	b *HashiBackend
}

func (s *peerAPIServer) listen(ip netip.Addr) (ln net.Listener, err error) {
	// Androidは何らかの理由で、peerapiリスナーの作成にしばしば問題を起こすらしい
	if runtime.GOOS == "android" {
		return newFakePeerAPIListener(ip), nil
	}

	ipStr := ip.String()

	var lc net.ListenConfig
	if initListenConfig != nil {
		// tunnameからifaceのindexを取得する
		interfaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}

		var i *net.Interface
		for _, iface := range interfaces {
			if iface.Name == s.b.sys.Dialer.Get().GetTUNName() {
				i = &iface
			}
		}

		// iOS/macOSではlc.Controlフックをsetsockoptに設定し、
		// バインド先のインターフェース・インデックスを設定することで、ネットワーク・サンドボックスから抜け出すことができるらしい
		if err := initListenConfig(&lc, ip, i); err != nil {
			return nil, err
		}
		if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
			ipStr = ""
		}
	}

	if s.b.sys.IsNetstack() {
		ipStr = ""
	}

	tcp4or6 := "tcp4"
	if ip.Is6() {
		tcp4or6 = "tcp6"
	}

	// Fall back to some random ephemeral port.
	ln, err = lc.Listen(context.Background(), tcp4or6, net.JoinHostPort(ipStr, "0"))

	// And if we're on a platform with netstack (anything but iOS), then just fallback to netstack.
	if err != nil && runtime.GOOS != "ios" {
		return newFakePeerAPIListener(ip), nil
	}
	return ln, err
}

type peerAPIListener struct {
	ps *peerAPIServer
	ip netip.Addr
	hb *HashiBackend

	ln net.Listener

	urlStr string
	port   int
}

func (pln *peerAPIListener) serve() {
	if pln.ln == nil {
		return
	}
	defer pln.ln.Close()
	log := pln.hb.logger
	for {
		// peer api serverのlistenerを使用
		c, err := pln.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			log.Logger.Infof("peerapi.Accept: %v", err)
			return
		}
		ta, ok := c.RemoteAddr().(*net.TCPAddr)
		if !ok {
			c.Close()
			log.Logger.Infof("peerapi: unexpected RemoteAddr %#v", c.RemoteAddr())
			continue
		}
		ipp := ipaddr.Unmap(ta.AddrPort())
		if !ipp.IsValid() {
			log.Logger.Infof("peerapi: bogus TCPAddr %#v", ta)
			c.Close()
			continue
		}
		pln.ServeConn(ipp, c)
	}
}

// serveから呼ばれる
// listenerをAcceptしてhttp serverを立ち上げる
func (pln *peerAPIListener) ServeConn(src netip.AddrPort, c net.Conn) {
	// log := pln.hb.logger
	// peerNode, peerUser, ok := pln.lb.WhoIs(src)
	// if !ok {
	// 	logf("peerapi: unknown peer %v", src)
	// 	c.Close()
	// 	return
	// }
	// nm := pln.hb.NetMap()
	// if nm == nil || !nm.SelfNode.Valid() {
	// 	logf("peerapi: no netmap")
	// 	c.Close()
	// 	return
	// }

	// h := &peerAPIHandler{
	// 	ps:         pln.ps,
	// 	isSelf:     nm.SelfNode.User() == peerNode.User(),
	// 	remoteAddr: src,
	// 	selfNode:   nm.SelfNode,
	// 	peerNode:   peerNode,
	// 	peerUser:   peerUser,
	// }
	// httpServer := &http.Server{
	// 	Handler: h,
	// }
	// if addH2C != nil {
	// 	addH2C(httpServer)
	// }
	// // peerAPIHandlerのServeHTTPが呼ばれる
	// go httpServer.Serve(netutil.NewOneConnListener(c, nil))
}

func (pln *peerAPIListener) Close() error {
	if pln.ln != nil {
		return pln.ln.Close()
	}
	return nil
}

func newFakePeerAPIListener(ip netip.Addr) net.Listener {
	return &fakePeerAPIListener{
		addr:   net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, 1)),
		closed: make(chan struct{}),
	}
}

type fakePeerAPIListener struct {
	addr net.Addr

	closeOnce sync.Once
	closed    chan struct{}
}

func (fl *fakePeerAPIListener) Close() error {
	fl.closeOnce.Do(func() { close(fl.closed) })
	return nil
}

func (fl *fakePeerAPIListener) Accept() (net.Conn, error) {
	<-fl.closed
	return nil, net.ErrClosed
}

func (fl *fakePeerAPIListener) Addr() net.Addr { return fl.addr }
