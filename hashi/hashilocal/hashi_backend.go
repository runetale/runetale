// HashiBackendは
// Runetaleの主要部分である
// （controlclient経由）クラウドコントロールプレーン, runetale-server
// （rnengine経由）ネットワークデータプレーン
// ユーザー向けのUIとCLI間の架け橋
// peer apiとhashigo apiをserveするのに必要な構造体
package hashilocal

import (
	"net"
	"net/netip"
	"strconv"
	"sync"

	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/rsystemd"
	"github.com/runetale/runetale/types/netmap"
	"gvisor.dev/gvisor/pkg/tcpip"
)

// HashiBackendは、
// Runetaleの主要部分である
// （controlclient経由の）クラウドコントロールプレーン
// （rnengine経由の）ネットワークデータプレーン
// ユーザー向けのUIとCLI間の架け橋
// peer apiとhashigo apiをserveするのに必要な構造体
type HashiBackend struct {
	sys *rsystemd.System

	// このNodeのIPアドレスが入る
	// - GetPeerAPIPortを経由して、wireguardから来たパケットをnetstackでパケットを処理するかどうかに使う
	// - TCPHandlerForDst
	// その名の通り、peerをlistenする
	peerAPIListeners []*peerAPIListener

	// hashigo-apiがアクセスするpeer API Server
	peerAPIServer *peerAPIServer

	netMap *netmap.NetworkMap

	mu sync.Mutex

	logger *log.Logger
}

// GetPeerAPIPortは、指定されたIP上で動作しているpeerapiサーバーのポート番号を返します。
func (b *HashiBackend) GetPeerAPIPort(ip netip.Addr) (port uint16, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, pln := range b.peerAPIListeners {
		if pln.ip == ip {
			return uint16(pln.port), true
		}
	}
	return 0, false
}

// netstackのtcp forwardで使用する
// GetPeerAPIPortを使用して、peerAPIListnerが有効かどうか確認する
func (b *HashiBackend) TCPHandlerForDst(src, dst netip.AddrPort) (handler func(c net.Conn) error, opts []tcpip.SettableSocketOption) {
	return nil, nil
}

func (b *HashiBackend) Shutdown() {
}

// for peer api
//
// peerAPIListenerを初期化してserveする。plnのサーバーが立ち上がる。
// pln serverはこのpeerの情報を提供するためのapi server
// peerAPIServerをrunetaleIPでlistenする
// peerAPIListenerがrunetaleIPでserverをserveする
func (b *HashiBackend) initPeerAPIListener() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.netMap == nil {
		return
	}

	addrs := b.netMap.GetAddresses()

	// 無駄に再度Listenしないための処理
	if addrs.Len() == len(b.peerAPIListeners) {
		allSame := true
		for i, pln := range b.peerAPIListeners {
			if pln.ip != addrs.At(i).Addr() {
				allSame = false
				break
			}
		}
		if allSame {
			// Nothing to do.
			return
		}
	}

	b.closePeerAPIListenersLocked()

	if !b.netMap.SelfNode.IsValid() || b.netMap.GetAddresses().Len() == 0 {
		return
	}

	ps := &peerAPIServer{
		b: b,
	}
	b.peerAPIServer = ps

	for i := range addrs.Len() {
		a := addrs.At(i)
		var ln net.Listener
		var err error

		// TCPリスナーを作成
		ln, err = ps.listen(a.Addr())
		if err != nil {
			b.logger.Logger.Infof("[unexpected] peerapi listen(%q) error: %v", a.Addr(), err)
			continue
		}
		pln := &peerAPIListener{
			// peer api server
			ps: ps,
			// this node ip
			ip: a.Addr(),
			// peer api server listener
			ln: ln,
			// local backend
			hb: b,
		}

		pln.port = ln.Addr().(*net.TCPAddr).Port

		pln.urlStr = "http://" + net.JoinHostPort(a.Addr().String(), strconv.Itoa(pln.port))
		b.logger.Logger.Infof("peerapi: serving on %s", pln.urlStr)

		// runetaleのIPアドレスでhttp serverを立ち上げる
		// listenerの接続を待ち受ける
		go pln.serve()
		b.peerAPIListeners = append(b.peerAPIListeners, pln)
	}
}

func (b *HashiBackend) closePeerAPIListenersLocked() {
	b.peerAPIServer = nil
	for _, pln := range b.peerAPIListeners {
		pln.Close()
	}
	b.peerAPIListeners = nil
}

// wg deviceとrouterの設定
func (b *HashiBackend) authReconfig() {

}
