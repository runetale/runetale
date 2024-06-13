package netstack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runetale/runetale/atomics"
	"github.com/runetale/runetale/hashi/hashilocal"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/net/ipaddr"
	"github.com/runetale/runetale/net/packet"
	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/net/rntun/filter"
	"github.com/runetale/runetale/net/rtaddr"
	"github.com/runetale/runetale/net/runedial"
	"github.com/runetale/runetale/platform"
	"github.com/runetale/runetale/rnengine"
	"github.com/runetale/runetale/rnengine/wonderwall"
	"github.com/runetale/runetale/semaphore"
	"github.com/runetale/runetale/types/ipproto"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/refs"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const nicID = 1
const maxUDPPacketSize = rntun.MaxPacketSize

func init() {
	mode := os.Getenv("DEBUG_NETSTACK_LEAK_MODE")
	if mode == "" {
		return
	}
	var lm refs.LeakMode
	if err := lm.Set(mode); err != nil {
		panic(err)
	}
	refs.SetLeakMode(lm)
}

// netstackのSYNパケット（接続要求）を同時に処理できる最大数
func maxInFlightConnection() int {
	if platform.IsMobile() {
		return 1024 // previous global value
	}
	switch platform.OS() {
	case "linux":
		// subnet routerは基本的にlinuxで動作しているので高い値を返す
		return 4096
	default:
		// 他はそれなりに高い値を返す
		return 2048
	}
}

type Netstack struct {
	// onlyNetstack?
	HandleNetstack bool

	// Subnet且つ自身のノードのIPアドレスでない場合
	// OSではなくnetstackがサブネットルーターを処理するかどうか
	// OSがサブネット・ルーターをサポートしていない場合（例：Windows）
	// またはユーザーが明示的に要求した場合（例：-tun=userspace-networking)
	HandleSubnetRouter bool

	hashigo *hashilocal.HashiBackend // or nil

	ipstack *stack.Stack
	linkEP  *channel.Endpoint
	tundev  *rntun.Wrapper
	engine  rnengine.Engine
	wc      *wonderwall.Conn
	dialer  *runedial.Dialer
	logger  *log.Logger

	mu sync.Mutex

	ctx       context.Context
	ctxCancel context.CancelFunc

	// このNodeのIPアドレス
	isStoredAddr atomics.AtomicValue[func(netip.Addr) bool]

	// connsInFlightByClientは、クライアント("Runetale")IPごとの飛行中接続数を追跡します。remoteIPをトラッキング
	// リミット制限を監視するために必要
	// コネクションの準備ができたら、削除される
	connsInFlightByClient map[netip.Addr]int

	// gvisorのtcp forwardereのinflightは保留中パケットしか保持しない
	// inflightでコネクションを確立しようとしてる、飛行中の状態を管理するために必要
	// コネクションの準備ができたら、削除される
	packetsInFlight map[stack.TransportEndpointID]struct{}

	// 別のサブネット空間のIPアドレスもプロキシするようにする
	connsOpenBySubnetIP map[netip.Addr]int

	// localbackendで設定されこのNodeのIPのtcp connectionのポートをStoreする
	// TCPコネクションを確立する時とした後に適切なポートにパケットをハンドリングするために必要
	// shouldProcessInboundで使用する
	peerapiPort4Atomic atomic.Uint32
	peerapiPort6Atomic atomic.Uint32
}

func NewNetstack(tundev *rntun.Wrapper, e rnengine.Engine, w *wonderwall.Conn, dialer *runedial.Dialer, logger *log.Logger) (*Netstack, error) {
	if tundev == nil {
		return nil, errors.New("nil tundev")
	}
	if e == nil {
		return nil, errors.New("nil Engine")
	}
	if w == nil {
		return nil, errors.New("nil wonderwall.Conn")
	}
	if dialer == nil {
		return nil, errors.New("nil Dialer")
	}
	if logger == nil {
		return nil, errors.New("nil logger")
	}

	// setup gvisor ipstack
	ipstack := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})
	sackEnabledOpt := tcpip.TCPSACKEnabled(true)
	tcpipErr := ipstack.SetTransportProtocolOption(tcp.ProtocolNumber, &sackEnabledOpt)
	if tcpipErr != nil {
		return nil, fmt.Errorf("could not enable TCP SACK: %v", tcpipErr)
	}

	// WindowsはTCPのRACKの再送のパフォーマンスが悪い
	// 輻輳ウィンドウが減少する
	// リアルタイムで処理されないらしい
	if runtime.GOOS == "windows" {
		tcpRecoveryOpt := tcpip.TCPRecovery(0)
		tcpipErr = ipstack.SetTransportProtocolOption(tcp.ProtocolNumber, &tcpRecoveryOpt)
		if tcpipErr != nil {
			return nil, fmt.Errorf("could not disable TCP RACK: %v", tcpipErr)
		}
	}
	// アドレスを指定しないのは、
	linkEP := channel.New(512, uint32(rntun.DEFAULTMTU), "")
	if tcpipProblem := ipstack.CreateNIC(nicID, linkEP); tcpipProblem != nil {
		return nil, fmt.Errorf("could not create netstack NIC: %v", tcpipProblem)
	}

	// デフォルトでは、netstack NICは登録されたIPのパケットしか受け付けない。
	// 到着したパケットに基づいて動的にIPを登録する場合もあるため、NICはすべての受信パケットを受け入れる必要がある。
	// WireGuardはRunetale宛のパケットのみを送信するため、NICが意図しないものを受信することはない。
	ipstack.SetPromiscuousMode(nicID, true)
	// IPv4とIPv6のデフォルトルートを追加し、Runetale側からのすべての着信パケットを、使用する1つの偽NICで処理できるようにする。
	ipv4Subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice(make([]byte, 4)), tcpip.MaskFromBytes(make([]byte, 4)))
	if err != nil {
		return nil, fmt.Errorf("could not create IPv4 subnet: %v", err)
	}
	ipv6Subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice(make([]byte, 16)), tcpip.MaskFromBytes(make([]byte, 16)))
	if err != nil {
		return nil, fmt.Errorf("could not create IPv6 subnet: %v", err)
	}
	ipstack.SetRouteTable([]tcpip.Route{
		{
			Destination: ipv4Subnet,
			NIC:         nicID,
		},
		{
			Destination: ipv6Subnet,
			NIC:         nicID,
		},
	})

	// impl Netstack
	ns := &Netstack{
		ipstack:               ipstack,
		linkEP:                linkEP,
		tundev:                tundev,
		engine:                e,
		wc:                    w,
		dialer:                dialer,
		logger:                logger,
		connsInFlightByClient: make(map[netip.Addr]int),
		packetsInFlight:       make(map[stack.TransportEndpointID]struct{}),
	}
	ns.ctx, ns.ctxCancel = context.WithCancel(context.Background())

	ns.isStoredAddr.Store(func(ip netip.Addr) bool { return false })

	ns.tundev.PostFilterPacketInboundFromWireGuard = ns.injectInbound
	ns.tundev.ReadFilterPacketOutboundToWireGuardNetstackIntercept = ns.handleLocalPackets

	return ns, nil
}

func (ns *Netstack) Start() error {
	const tcpReceiveBufferSize = 0

	// netstackにtcpとudpのパケットが転送されるように設定
	tcpFwd := tcp.NewForwarder(ns.ipstack, tcpReceiveBufferSize, maxInFlightConnection(), ns.tcpForwardHandler)
	udpFwd := udp.NewForwarder(ns.ipstack, ns.udpForwardHandler)

	// wrapTCPProtocolHandlerはtcpForwardHandlerのTCPコネクションが開始されるまでのpacketsInFlightを正確にハンドリングするために必要なハンドラー関数
	// DialContextTCPを読んだ時によばれる
	ns.ipstack.SetTransportProtocolHandler(tcp.ProtocolNumber, ns.wrapTCPProtocolHandler(tcpFwd.HandlePacket))
	// wrapUDPProtocolHandlerはudpForwardHandlerのUDPコネクションが開始されるまでのpacketsInFlightを正確にハンドリングするために必要なハンドラー関数
	// DialContextUDPを読んだ時によばれる
	ns.ipstack.SetTransportProtocolHandler(udp.ProtocolNumber, ns.wrapUDPProtocolHandler(udpFwd.HandlePacket))
	go ns.inject()
	return nil
}

func (ns *Netstack) Close() error {
	ns.ctxCancel()
	ns.ipstack.Close()
	ns.ipstack.Wait()
	return nil
}

// if tcpipProblem := ipstack.CreateNIC(nicID, linkEP); tcpipProblem != nil {
// と ns.jnject()のlinkEP.ReadContextのおかげでnetstack経由で普通のExternal TCP/IP or UDPの通信をWireGuardのデバイス経由で通信することが可能になる
// hashigo api経由で呼ばれたIPとPortにnetstackがダイアルしに行く
func (ns *Netstack) DialContextTCP(ctx context.Context, ipp netip.AddrPort) (*gonet.TCPConn, error) {
	remoteAddress := tcpip.FullAddress{
		NIC:  nicID,
		Addr: tcpip.AddrFromSlice(ipp.Addr().AsSlice()),
		Port: ipp.Port(),
	}
	var ipType tcpip.NetworkProtocolNumber
	if ipp.Addr().Is4() {
		ipType = ipv4.ProtocolNumber
	} else {
		ipType = ipv6.ProtocolNumber
	}

	return gonet.DialContextTCP(ctx, ns.ipstack, remoteAddress, ipType)
}

func (ns *Netstack) DialContextUDP(ctx context.Context, ipp netip.AddrPort) (*gonet.UDPConn, error) {
	remoteAddress := &tcpip.FullAddress{
		NIC:  nicID,
		Addr: tcpip.AddrFromSlice(ipp.Addr().AsSlice()),
		Port: ipp.Port(),
	}
	var ipType tcpip.NetworkProtocolNumber
	if ipp.Addr().Is4() {
		ipType = ipv4.ProtocolNumber
	} else {
		ipType = ipv6.ProtocolNumber
	}

	return gonet.DialUDP(ns.ipstack, nil, remoteAddress, ipType)
}

func (ns *Netstack) getNetIPAddrFromNetstackIP(remoteAddr tcpip.Address) netip.Addr {
	switch remoteAddr.Len() {
	case 4:
		s := remoteAddr.As4()
		return ipaddr.IPv4(s[0], s[1], s[2], s[3])
	case 16:
		s := remoteAddr.As16()
		return netip.AddrFrom16(s).Unmap()
	}
	return netip.Addr{}
}

// wrapTCPProtocolHandlerでインクリメントされたconnsInFlightByClientデクリメントする
// packetsInFlightも削除する
func (ns *Netstack) decrementInFlightTCPForward(endpointID stack.TransportEndpointID, remoteAddr netip.Addr) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	delete(ns.packetsInFlight, endpointID)

	was := ns.connsInFlightByClient[remoteAddr]
	newVal := was - 1
	if newVal == 0 {
		// free map of connsInFlightByClient
		delete(ns.connsInFlightByClient, remoteAddr)
	} else {
		ns.connsInFlightByClient[remoteAddr] = newVal
	}
}

// tcpのforwardingの設定を行う
// backendもしくはnetstack内でtcp proxyを開始する
func (ns *Netstack) tcpForwardHandler(r *tcp.ForwarderRequest) {
	transportEndpointID := r.ID()

	clientRemoteIP := ns.getNetIPAddrFromNetstackIP(transportEndpointID.RemoteAddress)
	if !clientRemoteIP.IsValid() {
		// sends a RST,
		// TCP接続を強制的に終了
		r.Complete(true)
		return
	}

	inFlightCompleted := false
	defer func() {
		if !inFlightCompleted {
			ns.decrementInFlightTCPForward(transportEndpointID, clientRemoteIP)
		}
	}()

	// clientRemotePort := transportEndpointID.RemotePort
	// clientRemoteAddrPort := netip.AddrPortFrom(clientRemoteIP, clientRemotePort)

	dialIP := ns.getNetIPAddrFromNetstackIP(transportEndpointID.LocalAddress)
	// dstAddrPort := netip.AddrPortFrom(dialIP, transportEndpointID.LocalPort)

	isRunetaleIP := rtaddr.IsRunetaleIP(dialIP)

	var wq waiter.Queue

	// この関数はforwardTCPや、Backendとの接続に使用される。remoteのエンドポイントを渡す
	getConnOrReset := func(opts ...tcpip.SettableSocketOption) *gonet.TCPConn {
		endpoint, err := r.CreateEndpoint(&wq)
		if err != nil {
			ns.logger.Logger.Infof("CreateEndpoint error %s: %v", showE2EHostPort(transportEndpointID), err)
			r.Complete(true)
			return nil
		}
		// TCP接続は閉じない、gvisorの中のinflightのmapから削除される
		// リクエストの追跡を停止する
		r.Complete(false)
		for _, opt := range opts {
			endpoint.SetSockOpt(opt)
		}

		// アイドル状態の接続がタイムアウトするようにする。
		// ピアが接続を忘れた場合や完全にオフラインになった場合に対処するため。
		// ユーザースペースからはアプリケーションが設定している可能性のあるキープアライブ設定を確認できないため
		// 常にキープアライブを実行するのが最善
		endpoint.SocketOptions().SetKeepAlive(true)

		// コネクションを返すタイミングはすでに飛行完了しているので、decrementする
		ns.decrementInFlightTCPForward(transportEndpointID, clientRemoteIP)
		inFlightCompleted = true

		return gonet.NewTCPConn(&wq, endpoint)
	}

	// todo: (snt) localのbakcendができたタイミングで実装
	// if ns.lb != nil {
	// 	handler, opts := ns.lb.TCPHandlerForDst(clientRemoteAddrPort, dstAddrPort)
	// 	if handler != nil {
	// 		c := getConnOrReset(opts...) // will send a RST if it fails
	// 		if c == nil {
	// 			return
	// 		}
	// 		handler(c)
	// 		return
	// 	}
	// }

	if isRunetaleIP {
		dialIP = ipaddr.IPv4(127, 0, 0, 1)
	}
	dialAddr := netip.AddrPortFrom(dialIP, uint16(transportEndpointID.LocalPort))

	if !ns.forwardTCP(getConnOrReset, clientRemoteIP, &wq, dialAddr) {
		// reset segmentの送信
		r.Complete(true)
	}
}

func (ns *Netstack) forwardTCP(getClient func(...tcpip.SettableSocketOption) *gonet.TCPConn, clientRemoteIP netip.Addr, wq *waiter.Queue, dialAddr netip.AddrPort) (handled bool) {
	dialAddrStr := dialAddr.String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	waitEntry, notifyCh := waiter.NewChannelEntry(waiter.EventHUp)
	wq.EventRegister(&waitEntry)
	defer wq.EventUnregister(&waitEntry)
	done := make(chan bool)

	// eventHUPが通知されない場合のハンドリング
	defer close(done)

	go func() {
		select {
		case <-notifyCh:
		case <-done:
		}
		cancel()
	}()

	// Attempt to dial the outbound connection before we accept the inbound one.
	var dialFunc func(context.Context, string, string) (net.Conn, error)
	var stdDialer net.Dialer
	dialFunc = stdDialer.DialContext

	server, err := dialFunc(ctx, "tcp", dialAddrStr)
	if err != nil {
		ns.logger.Logger.Infof("netstack: could not connect to local server at %s: %v", dialAddr.String(), err)
		return
	}
	defer server.Close()

	// 呼び出し元のクリーンアップを行う必要がないため、reset segmentを呼ぶ必要がない
	handled = true

	// TCPハンドシェイク開始準備完了
	// このタイミングでpacketsInFlightはデクリメントされる
	client := getClient()
	if client == nil {
		return
	}
	defer client.Close()

	// todo: (snt) netstackのproxymapにcacheする
	// backendLocalAddr := server.LocalAddr().(*net.TCPAddr)
	// backendLocalIPPort := netaddr.Unmap(backendLocalAddr.AddrPort())
	// ns.pm.RegisterIPPortIdentity(backendLocalIPPort, clientRemoteIP)
	// defer ns.pm.UnregisterIPPortIdentity(backendLocalIPPort)

	connClosed := make(chan error, 2)

	// clientとserverのproxyを開始
	go func() {
		_, err := io.Copy(server, client)
		connClosed <- err
	}()
	go func() {
		_, err := io.Copy(client, server)
		connClosed <- err
	}()
	err = <-connClosed
	if err != nil {
		ns.logger.Logger.Infof("proxy connection closed with error: %v", err)
	}
	ns.logger.Logger.Infof("netstack: forwarder connection to %s closed", dialAddrStr)
	return
}

// clientはnetstackのendpoint
// srcAddrにはremoteAddressが入る
// dstAddrにはlocalAddress, isLocalIPにStoreされていない場合は、管理していないIPなので直接プロキシを行う
func (ns *Netstack) forwardUDP(client *gonet.UDPConn, srcAddr, dstAddr netip.AddrPort) {
	port, srcPort := dstAddr.Port(), srcAddr.Port()

	var backendListenAddr *net.UDPAddr
	var backendRemoteAddr *net.UDPAddr

	isLocal := ns.isStoredIP(dstAddr.Addr())
	// local-ipaddressの場合は127.0.0.1でプロキシする
	// それ以外のIPの場合は (アドバタイズされたサブネットからの)直接プロキシする
	if isLocal {
		backendRemoteAddr = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(port)}
		backendListenAddr = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(srcPort)}
	} else {
		// このbackendRemoteAddrはrunetaleのnetworkのアドレスではないので、IPアドレスを使用して、直接プロキシするようにする
		backendRemoteAddr = net.UDPAddrFromAddrPort(dstAddr)
		if dstAddr.Addr().Is4() {
			backendListenAddr = &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: int(srcPort)}
		} else {
			backendListenAddr = &net.UDPAddr{IP: net.ParseIP("::"), Port: int(srcPort)}
		}
	}

	backendConn, err := net.ListenUDP("udp", backendListenAddr)
	if err != nil {
		ns.logger.Logger.Infof("netstack: could not bind local port %v: %v, trying again with random port", backendListenAddr.Port, err)
		backendListenAddr.Port = 0
		backendConn, err = net.ListenUDP("udp", backendListenAddr)
		if err != nil {
			ns.logger.Logger.Infof("netstack: could not create UDP socket, preventing forwarding to %v: %v", dstAddr, err)
			return
		}
	}

	backendLocalAddr := backendConn.LocalAddr().(*net.UDPAddr)
	backendLocalIPPort := netip.AddrPortFrom(backendListenAddr.AddrPort().Addr().Unmap().WithZone(backendLocalAddr.Zone), backendLocalAddr.AddrPort().Port())
	if !backendLocalIPPort.IsValid() {
		ns.logger.Logger.Infof("could not get backend local IP:port from %v:%v", backendLocalAddr.IP, backendLocalAddr.Port)
	}
	ctx, cancel := context.WithCancel(context.Background())

	idleTimeout := 2 * time.Minute

	timer := time.AfterFunc(idleTimeout, func() {
		ns.logger.Logger.Infof("netstack: UDP session between %s and %s timed out", backendListenAddr, backendRemoteAddr)
		cancel()
		client.Close()
		backendConn.Close()
	})
	extend := func() {
		timer.Reset(idleTimeout)
	}

	// client(endpoint)にclientAddrを指定してbackendConnのパケットを流す
	startPacketCopy(ctx, cancel, client, net.UDPAddrFromAddrPort(srcAddr), backendConn, ns.logger, extend)
	// backendConnにbackendRemoteAddrを指定してclientのパケットを流す
	startPacketCopy(ctx, cancel, backendConn, backendRemoteAddr, client, ns.logger, extend)

	if isLocal {
		// Wait for the copies to be done before decrementing the
		// subnet address count to potentially remove the route.
		<-ctx.Done()
		ns.removeSubnetAddress(dstAddr.Addr())
	}
}

func (ns *Netstack) udpForwardHandler(r *udp.ForwarderRequest) {
	transportEndpointID := r.ID()
	var wq waiter.Queue
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		ns.logger.Logger.Infof("acceptUDP: could not create endpoint: %v", err)
		return
	}

	dstAddr, ok := getAddrPort(transportEndpointID.LocalAddress, transportEndpointID.LocalPort)
	if !ok {
		ep.Close()
		return
	}
	srcAddr, ok := getAddrPort(transportEndpointID.RemoteAddress, transportEndpointID.RemotePort)
	if !ok {
		ep.Close()
		return
	}

	// TODO:(snt)
	// backend serverのudpHadlerForFlowでlistenするようにする
	// if get := ns.GetUDPHandlerForFlow; get != nil {
	// 	h, intercept := get(srcAddr, dstAddr)
	// 	if intercept {
	// 		if h == nil {
	// 			ep.Close()
	// 			return
	// 		}
	// 		go h(gonet.NewUDPConn(&wq, ep))
	// 		return
	// 	}
	// }

	c := gonet.NewUDPConn(&wq, ep)
	go ns.forwardUDP(c, srcAddr, dstAddr)
}

var udpBufPool = &sync.Pool{
	New: func() any {
		b := make([]byte, maxUDPPacketSize)
		return &b
	},
}

func startPacketCopy(ctx context.Context, cancel context.CancelFunc, dst net.PacketConn, dstAddr net.Addr, src net.PacketConn, logger *log.Logger, extend func()) {
	go func() {
		defer cancel() // tear down the other direction's copy

		bufp := udpBufPool.Get().(*[]byte)
		defer udpBufPool.Put(bufp)
		pkt := *bufp

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, srcAddr, err := src.ReadFrom(pkt)
				if err != nil {
					if ctx.Err() == nil {
						logger.Logger.Infof("read packet from %s failed: %v", srcAddr, err)
					}
					return
				}
				_, err = dst.WriteTo(pkt[:n], dstAddr)
				if err != nil {
					if ctx.Err() == nil {
						logger.Logger.Infof("write packet to %s failed: %v", dstAddr, err)
					}
					return
				}
				extend()
			}
		}
	}()
}

type protocolHandlerFunc func(stack.TransportEndpointID, *stack.PacketBuffer) bool

// このハンドラーが実行された後にtcpForwardHandlerがよばれる
// tcpForwardHandlerのgetConnOrReset関数が実行されたときに
// decrementInFlightTCPForwardを使用して、飛行中のInflighを削除とデクリメントを行う
func (ns *Netstack) wrapTCPProtocolHandler(h protocolHandlerFunc) protocolHandlerFunc {
	// もしtrueなら、TCP接続はトランスポートレイヤーに受け入れられ、 acceptTCPハンドラなどを通過する
	// falseの場合、パケットはドロップされ、TCP接続は拒否される(通常はICMP Port UnreachableまたはICMP Protocol Unreachableメッセージで)
	return func(transportEndpointID stack.TransportEndpointID, pb *stack.PacketBuffer) (handled bool) {
		localIP, ok := netip.AddrFromSlice(transportEndpointID.LocalAddress.AsSlice())
		if !ok {
			ns.logger.Logger.Infof("netstack: could not parse local address for incoming connection")
			return false
		}
		localIP = localIP.Unmap()

		remoteIP, ok := netip.AddrFromSlice(transportEndpointID.RemoteAddress.AsSlice())
		if !ok {
			ns.logger.Logger.Infof("netstack: could not parse remote address for incoming connection")
			return false
		}

		// tcpForwardHandlerが呼び出された時に
		// decrementInFlightTCPForwardでデクリメントされる。
		ns.mu.Lock()
		// すでにこのTransportのパケットをハンドリングしている場合
		if _, ok := ns.packetsInFlight[transportEndpointID]; ok {
			ns.mu.Unlock()
			return true
		}

		// inFlightの上限を確認する
		inFlight := ns.connsInFlightByClient[remoteIP]
		tooManyInFlight := inFlight >= maxInFlightConnection()
		if !tooManyInFlight {
			ns.connsInFlightByClient[remoteIP]++
		}

		// packetsInFlightを飛行中の状態にする
		ns.packetsInFlight[transportEndpointID] = struct{}{}
		ns.mu.Unlock()

		// このクライアントのinflight接続が多すぎる場合は新しい接続を開始しない
		if tooManyInFlight {
			ns.logger.Logger.Infof("netstack: ignoring a new TCP connection from %v to %v because the client already has %d in-flight connections", localIP, remoteIP, inFlight)
			return false
		}

		// このパケットがラップしているインナーハンドラ(`h`)によって処理されなかった場合、
		// クライアントごとのインフライトカウントをデクリメントし、トラッキングマップからIDを削除する必要がある
		defer func() {
			if !handled {
				ns.mu.Lock()
				delete(ns.packetsInFlight, transportEndpointID)
				ns.connsInFlightByClient[remoteIP]--
				new := ns.connsInFlightByClient[remoteIP]
				ns.mu.Unlock()
				ns.logger.Logger.Infof("netstack: decrementing connsInFlightByClient[%v] because the packet was not handled; new value is %d", remoteIP, new)
			}
		}()

		// NOTE:
		// isLocalIPはNode.Addressesから取得した値が入っている、そのIPがStoreされているか確認する
		// Storeされていなければ、提供されたIPとPortとTCPハンドシェイクするようにする
		// つまりWireGuardを使わない、NetstackのProxyとしても動作させる。gvisorの機能をうまく使う
		if !ns.isStoredIP(localIP) {
			ns.addSubnetAddress(localIP)
		}

		return h(transportEndpointID, pb)
	}
}

func (ns *Netstack) wrapUDPProtocolHandler(h protocolHandlerFunc) protocolHandlerFunc {
	return func(transportEndpointID stack.TransportEndpointID, p *stack.PacketBuffer) bool {
		addr := transportEndpointID.LocalAddress
		ip, ok := netip.AddrFromSlice(addr.AsSlice())
		if !ok {
			ns.logger.Logger.Infof("netstack: could not parse local address for incoming connection")
			return false
		}

		// NOTE:
		// isLocalIPはNode.Addressesから取得した値が入っている、そのIPがStoreされているか確認する
		// Storeされていなければ、提供されたIPとPortとTCPハンドシェイクするようにする
		// つまりWireGuardを使わない、NetstackのProxyとしても動作させる。gvisorの機能をうまく使う
		ip = ip.Unmap()
		if !ns.isStoredIP(ip) {
			ns.addSubnetAddress(ip)
		}
		return h(transportEndpointID, p)
	}
}

func (ns *Netstack) shouldHandlePing(p *packet.Parsed) (_ netip.Addr, ok bool) {
	if !p.IsEchoRequest() {
		return netip.Addr{}, false
	}

	dstIP := p.Dst.Addr()

	// pingがrunetale宛の場合は処理しない
	if !rtaddr.IsRunetaleIP(dstIP) {
		return netip.Addr{}, false
	}

	return dstIP, true
}

var userPingSem = semaphore.NewSemaphore(20)

type userPingDirection int

const (
	// userPingDirectionOutboundはPongパケットが「送信」
	// このノードからWireGuard経由でピアに送信される場合に使用されます
	userPingDirectionOutbound userPingDirection = iota
	// userPingDirectionInboundはPingパケットを「受信」
	// （Runetaleからこのホスト上の別のプロセスへ）する場合に使用される
	userPingDirectionInbound
)

// userPingはdstIPへのpingを試み、成功したらpingResPktをtundevに注入する。
// カーネル・サポートやrawソケット・アクセスがない場合のユーザー・スペース/ネットスタック・モードで使う。
// そのため、これはpingコマンドを実行するという、間抜けなことをする。
// 効率的ではないので、一度に実行されるpingの数を制限している。
// この関数はインターネットが機能しているかどうかを確認するために時々しかpingを使用しないというものである。
// アップル・プラットフォームでは、この関数はpingコマンドを実行しない。代わりに、非特権pingを送信する。
// pingが成功した場合、'direction'パラメータは、応答の "pong "パケットをどこに書き込むかを決定するために使用され。
func (ns *Netstack) userPing(dstIP netip.Addr, pingResPkt []byte, direction userPingDirection) {
	if !userPingSem.TryAcquire() {
		return
	}
	defer userPingSem.Release()

	// sent to ping
	now := time.Now()
	err := ns.sendOutboundUserPing(dstIP, 3*time.Second)
	d := time.Since(now)
	if err != nil {
		if d < time.Second/2 {
			ns.logger.Logger.Infof("exec ping of %v failed in %v: %v", dstIP, d, err)
		}
		return
	}

	// after ping complete, pong packet notifiy to tundev
	if direction == userPingDirectionOutbound {
		if err := ns.tundev.InjectOutbound(pingResPkt); err != nil {
			ns.logger.Logger.Infof("InjectOutbound ping response: %v", err)
		}
	} else if direction == userPingDirectionInbound {
		if err := ns.tundev.InjectInboundCopy(pingResPkt); err != nil {
			ns.logger.Logger.Infof("InjectInboundCopy ping response: %v", err)
		}
	}
}

// wireguardでWriteされたパケットをNetstackに注入している
func (ns *Netstack) injectInbound(p *packet.Parsed, t *rntun.Wrapper) filter.Response {
	if ns.ctx.Err() != nil {
		return filter.DropSilently
	}

	// trueの場合はnetstackにパケットをinboundする
	if !ns.shouldProcessInbound(p, t) {
		return filter.Accept
	}

	destIP := p.Dst.Addr()

	// netstackがICMPエコーパケットを処理すべきかどうかと、
	// pingされるべきIPアドレスを返す。
	pingIP, handlePing := ns.shouldHandlePing(p)
	if handlePing {
		var pong []byte
		if destIP.Is4() {
			h := p.ICMP4Header()
			h.ToResponse()
			pong = packet.Generate(&h, p.Payload())
		} else if destIP.Is6() {
			ns.logger.Logger.Infof("ping inject inbound not supported ipv6")
			return filter.Drop
		}
		go ns.userPing(pingIP, pong, userPingDirectionOutbound)
		return filter.DropSilently
	}

	var pn tcpip.NetworkProtocolNumber
	switch p.IPVersion {
	case 4:
		pn = header.IPv4ProtocolNumber
	case 6:
		pn = header.IPv6ProtocolNumber
	}

	// wireguardから送られてくるパケットを、netstackのpacket bufferに変換する
	packetBuf := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(bytes.Clone(p.Buffer())),
	})

	// linkEPにWireGuardのパケットを注入する。
	ns.linkEP.InjectInbound(pn, packetBuf)
	packetBuf.DecRef()

	return filter.DropSilently
}

// WireguardがReadしたパケットを
// filterPacketOutboundToWireGuard経由で受け取る
// filterPacketOutboundToWireGuardはRead関数で呼ばれる。
// vectorOutboundのチャネルで着信したパケットがこの関数に到着する

// todo: (snt) acceptパターンを追加？
func (ns *Netstack) handleLocalPackets(p *packet.Parsed, t *rntun.Wrapper) filter.Response {
	if ns.ctx.Err() != nil {
		return filter.DropSilently
	}

	dstIP := p.Dst.Addr()

	var pn tcpip.NetworkProtocolNumber
	switch p.IPVersion {
	case 4:
		pn = header.IPv4ProtocolNumber
	case 6:
		pn = header.IPv6ProtocolNumber
	}

	// netstackがICMPエコーパケットを処理すべきかどうかと、
	// pongされるべきIPアドレスを返す。
	pingIP, handlePing := ns.shouldHandlePing(p)
	if handlePing {
		var pong []byte
		if dstIP.Is4() {
			h := p.ICMP4Header()
			h.ToResponse()
			pong = packet.Generate(&h, p.Payload())
		} else if dstIP.Is6() {
			ns.logger.Logger.Infof("ping inject inbound not supported ipv6")
			return filter.Drop
		}
		go ns.userPing(pingIP, pong, userPingDirectionInbound)
		return filter.DropSilently
	}

	packetBuf := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(bytes.Clone(p.Buffer())),
	})
	ns.linkEP.InjectInbound(pn, packetBuf)
	packetBuf.DecRef()
	return filter.DropSilently
}

// netstackのパケットをwireguardにinjectする
func (ns *Netstack) inject() {
	for {
		packet := ns.linkEP.ReadContext(ns.ctx)
		if packet.IsNil() {
			if ns.ctx.Err() != nil {
				return
			}
			continue
		}

		sendToHost := ns.shouldSendToHost(packet)
		if sendToHost {
			// Writeが呼ばれる
			if err := ns.tundev.InjectInboundPacketBuffer(packet); err != nil {
				ns.logger.Logger.Infof("netstack inject inbound: %v", err)
				return
			}
		} else {
			// 別のノードに送信する時
			// device wrapperのReadが呼ばれる、netstackのパケットをReadメソッドに渡して
			// 別のWireGuardのNodeに送信している
			if err := ns.tundev.InjectOutboundPacketBuffer(packet); err != nil {
				ns.logger.Logger.Infof("netstack inject outbound: %v", err)
				return
			}
		}
	}
}

// shouldSendToHostは指定されたパケットをホスト(実行している現在のマシン)に送信するかどうかを判断する
// この場合、trueが返る
// パケットを外部に送信し、WireGuard経由で別のTノードに転送する場合は、falseが返る
func (ns *Netstack) shouldSendToHost(pkt *stack.PacketBuffer) bool {
	hdr := pkt.Network()
	switch v := hdr.(type) {
	case header.IPv4:
		srcIP := netip.AddrFrom4(v.SourceAddress().As4())
		// magicDNSの時にtrueを返している
		if ipaddr.IPv4(100, 100, 100, 100) == srcIP {
			return true
		}
	case header.IPv6:
	default:
	}
	return false
}

func showE2EHostPort(transportEndpointID stack.TransportEndpointID) string {
	local := net.JoinHostPort(transportEndpointID.LocalAddress.String(), strconv.Itoa(int(transportEndpointID.LocalPort)))
	remote := net.JoinHostPort(transportEndpointID.RemoteAddress.String(), strconv.Itoa(int(transportEndpointID.RemotePort)))
	return fmt.Sprintf("%s -> %s", remote, local)
}

// writeからのパケットをnetstackで処理するかどうかをチェックする
func (ns *Netstack) shouldProcessInbound(p *packet.Parsed, t *rntun.Wrapper) bool {
	dstIP := p.Dst.Addr()

	isLocal := ns.isStoredIP(dstIP)

	// DialContextTCPなどでリモートのtcpをproxyしている時は
	// 確認して、true
	if ns.hashigo != nil && p.IPProto == ipproto.TCP && isLocal {
		var peerAPIPort uint16
		// SYNフラグだけであるかどうかをチェック、つまり新しいTCP接続を開始する場合
		// peerListener(self node)にdstIPが含まれるか確認
		// 含まれている場合はそのtcp listnerのportを
		// peerAPIPortAtomicにStoreする
		// 	go pln.serve()でpeerapiでhttpserverを立ち上げて、srcと通信をできるようにしている？
		if p.TCPFlags&packet.TCPSynAck == packet.TCPSyn {
			if port, ok := ns.hashigo.GetPeerAPIPort(dstIP); ok {
				peerAPIPort = port
				ns.peerAPIPortAtomic(dstIP).Store(uint32(port))
			}
		} else {
			// SYNでない場合はハンドシェイクが確立されているので、peerAPIPortAtomicにStoreされている、portを取得する
			peerAPIPort = uint16(ns.peerAPIPortAtomic(dstIP).Load())
		}

		// storeされているportと受信した宛先のPortが合っているか確認。
		// つまりちゃんとnetstackの接続先から正しいこのノードのポートが指定されているか確認する
		dport := p.Dst.Port()
		if dport == peerAPIPort {
			return true
		}
	}

	// netstack且つ宛先がこのアドレスならtrue
	if ns.HandleNetstack && isLocal {
		return true
	}

	// subnet routingをする且つ宛先がこのアドレスではないならtrue
	if ns.HandleSubnetRouter && !isLocal {
		return true
	}

	return false
}

func (ns *Netstack) peerAPIPortAtomic(ip netip.Addr) *atomic.Uint32 {
	if ip.Is4() {
		return &ns.peerapiPort4Atomic
	} else {
		return &ns.peerapiPort6Atomic
	}
}

func (ns *Netstack) addSubnetAddress(ip netip.Addr) {
	ns.mu.Lock()
	ns.connsOpenBySubnetIP[ip]++
	needAdd := ns.connsOpenBySubnetIP[ip] == 1
	ns.mu.Unlock()
	if needAdd {
		pa := tcpip.ProtocolAddress{
			AddressWithPrefix: tcpip.AddrFromSlice(ip.AsSlice()).WithPrefix(),
		}
		if ip.Is4() {
			pa.Protocol = ipv4.ProtocolNumber
		} else if ip.Is6() {
			pa.Protocol = ipv6.ProtocolNumber
		}
		ns.ipstack.AddProtocolAddress(nicID, pa, stack.AddressProperties{
			PEB:        stack.CanBePrimaryEndpoint,
			ConfigType: stack.AddressConfigStatic,
		})
	}
}

func (ns *Netstack) removeSubnetAddress(ip netip.Addr) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.connsOpenBySubnetIP[ip]--
	if ns.connsOpenBySubnetIP[ip] == 0 {
		ns.ipstack.RemoveAddress(nicID, tcpip.AddrFromSlice(ip.AsSlice()))
		delete(ns.connsOpenBySubnetIP, ip)
	}
}

func (ns *Netstack) isStoredIP(ip netip.Addr) bool {
	return ns.isStoredAddr.Load()(ip)
}

func getAddrPort(addr tcpip.Address, port uint16) (ipp netip.AddrPort, ok bool) {
	if addr, ok := netip.AddrFromSlice(addr.AsSlice()); ok {
		return netip.AddrPortFrom(addr, port), true
	}
	return netip.AddrPort{}, false
}
