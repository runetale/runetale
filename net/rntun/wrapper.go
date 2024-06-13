// tun.DeviceのWrapper
// userspaceのネットワークスタックでパケットをフィルタリングする

package rntun

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/gaissmai/bart"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/net/packet"
	"github.com/runetale/runetale/net/rntun/filter"
	"github.com/runetale/runetale/net/rtaddr"
	"github.com/runetale/runetale/rnengine/wgconfig"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const MaxPacketSize = device.MaxContentSize

type peerConfigTable struct {
	// このNodeのIPv4のアドレス
	nativeAddr4 netip.Addr

	// nativeAddrに対してallowedIPsを持つ
	byIP bart.Table[*peerConfig]

	// masqAddrCounts is a count of peers by MasqueradeAsIP.
	masqAddrCounts map[netip.Addr]int
}

type peerConfig struct {
	dstMasqAddr4 netip.Addr
	isJailed     bool
}

type FilterFunc func(*packet.Parsed, *Wrapper) filter.Response

type Wrapper struct {
	tundev tun.Device
	isTAP  bool

	peerConfig atomic.Pointer[peerConfigTable]

	started atomic.Bool
	startCh chan struct{}

	// poolVectorで消費される
	vectorBuffer         [][]byte
	bufferConsumed       chan struct{}
	bufferConsumedMu     sync.Mutex
	bufferConsumedClosed bool

	// netstackからパケットを受け取るときにこのチャネルを使用する
	vectorOutbound chan tunVectorReadResult
	outboundMu     sync.Mutex
	outboundClosed bool

	eventsUpDown chan tun.Event
	eventsOther  chan tun.Event

	log *log.Logger

	closed   chan struct{}
	allClose sync.Once

	// for userspace handle local packets
	// packetをreadする時(受信)のフィルター
	// darwin or iosの場合にwireguardからのパケットを受信した時にhandle local packetに注入する
	ReadFilterPacketOutboundToUserspaceEngineIntercept FilterFunc

	// for netstack handle local packets
	// packetをreadする時(受信)のフィルター
	// パケットを受信した時にnetstackのhandle local packetに注入する
	ReadFilterPacketOutboundToWireGuardNetstackIntercept FilterFunc

	// for write filter
	// wireguardのdevice wrapperのパケットをWrite(送信)する時にacceptするかどうかを確認する
	// wireguardからのパケットをlinkEPに流す
	// netstackのinjectInbound or userspaceのechoRespondsAllが入る
	PostFilterPacketInboundFromWireGuard FilterFunc

	// for read filter
	// userspaceでセットされる
	// debug用なので、一旦無視でいいかも
	PostFilterPacketOutboundToWireGuard FilterFunc

	// todo (snt) 後で実装みる
	OnICMPEchoResponseReceived func(*packet.Parsed) bool
}

type tunInjectedRead struct {
	packet *stack.PacketBuffer
	data   []byte
}

type tunVectorReadResult struct {
	err      error
	data     [][]byte
	injected tunInjectedRead

	dataOffset int
}

func NewWrapperTap(log *log.Logger, tundev tun.Device) *Wrapper {
	return wrap(tundev, false, log)
}

func NewWrapper(log *log.Logger, tundev tun.Device) *Wrapper {
	return wrap(tundev, true, log)
}

// todo (snt)
// tap device, ethernetframeにも対応する
func wrap(tundev tun.Device, isTap bool, log *log.Logger) *Wrapper {
	w := &Wrapper{
		isTAP:  isTap,
		tundev: tundev,

		startCh: make(chan struct{}),

		bufferConsumed: make(chan struct{}, 1),

		vectorOutbound: make(chan tunVectorReadResult, 1),
		eventsUpDown:   make(chan tun.Event),
		eventsOther:    make(chan tun.Event),

		log:    log,
		closed: make(chan struct{}),
	}

	// 	windowsではtundev.Readがブロックされる可能性がある
	// その場合の対策として、未消費のパケットバッファをpollingしてReadに適切に流してあげる必要がある
	w.vectorBuffer = make([][]byte, tundev.BatchSize())
	for i := range w.vectorBuffer {
		w.vectorBuffer[i] = make([]byte, device.MaxMessageSize)
	}
	go w.pollVector()

	// tun.Eventをコピーして、eventチャネルに送る
	go w.pumpEvents()

	return w
}

func (w *Wrapper) pollVector() {
	sizes := make([]int, len(w.vectorBuffer))
	readOffset := PacketStartOffset

	for range w.bufferConsumed {
		for i := range w.vectorBuffer {
			w.vectorBuffer[i] = w.vectorBuffer[i][:cap(w.vectorBuffer[i])]
		}
		var n int
		var err error
		for n == 0 && err == nil {
			if w.isClosed() {
				return
			}
			n, err = w.tundev.Read(w.vectorBuffer[:], sizes, readOffset)
		}
		for i := range sizes[:n] {
			w.vectorBuffer[i] = w.vectorBuffer[i][:readOffset+sizes[i]]
		}
		w.sendVectorOutboundFromPoll(tunVectorReadResult{
			data:       w.vectorBuffer[:n],
			dataOffset: PacketStartOffset,
			err:        err,
		})
	}
}

// pollvectorからReadメソッドのvectorOutboundに送る
func (t *Wrapper) sendVectorOutboundFromPoll(r tunVectorReadResult) {
	t.outboundMu.Lock()
	defer t.outboundMu.Unlock()
	if t.outboundClosed {
		return
	}
	t.vectorOutbound <- r
}

func peerConfigTableFromWGConfig(wcfg *wgconfig.WgConfig) *peerConfigTable {
	if wcfg == nil {
		return nil
	}

	nativeAddr4 := rtaddr.FindRunetaleIPv4(wcfg.Addresses)
	if !nativeAddr4.IsValid() {
		return nil
	}

	ret := &peerConfigTable{
		nativeAddr4:    nativeAddr4,
		masqAddrCounts: make(map[netip.Addr]int),
	}

	// When using an exit node that requires masquerading, we need to
	// fill out the routing table with all peers not just the ones that
	// require masquerading.
	// exitNodeRequiresMasq := false // true if using an exit node and it requires masquerading
	// for _, p := range wcfg.Peers {
	// 	isExitNode := slices.Contains(p.AllowedIPs, tsaddr.AllIPv4()) || slices.Contains(p.AllowedIPs, tsaddr.AllIPv6())
	// 	if isExitNode {
	// 		hasMasqAddr := false ||
	// 			(p.V4MasqAddr != nil && p.V4MasqAddr.IsValid()) ||
	// 			(p.V6MasqAddr != nil && p.V6MasqAddr.IsValid())
	// 		if hasMasqAddr {
	// 			exitNodeRequiresMasq = true
	// 		}
	// 		break
	// 	}
	// }

	byIPSize := 0
	for i := range wcfg.Peers {
		p := &wcfg.Peers[i]

		// Build a routing table that configures DNAT (i.e. changing
		// the V4MasqAddr/V6MasqAddr for a given peer to the current
		// peer's v4/v6 IP).
		var addrToUse4 netip.Addr
		if p.V4MasqAddr != nil && p.V4MasqAddr.IsValid() {
			addrToUse4 = *p.V4MasqAddr
			ret.masqAddrCounts[addrToUse4]++
		}

		// If the exit node requires masquerading, set the masquerade
		// addresses to our native addresses.
		// if exitNodeRequiresMasq {
		// 	if !addrToUse4.IsValid() && nativeAddr4.IsValid() {
		// 		addrToUse4 = nativeAddr4
		// 	}
		// }

		if !addrToUse4.IsValid() {
			// NAT not required for this peer.
			continue
		}

		// Use the same peer configuration for each address of the peer.
		pc := &peerConfig{
			dstMasqAddr4: addrToUse4,
			isJailed:     p.IsJailed,
		}

		// Insert an entry into our routing table for each allowed IP.
		for _, ip := range p.AllowedIPs {
			ret.byIP.Insert(ip, pc)
			byIPSize++
		}
	}
	if byIPSize == 0 && len(ret.masqAddrCounts) == 0 {
		return nil
	}
	return ret
}

func (t *Wrapper) SetWGConfig(wcfg *wgconfig.WgConfig) {
	cfg := peerConfigTableFromWGConfig(wcfg)

	old := t.peerConfig.Swap(cfg)
	if !reflect.DeepEqual(old, cfg) {
		t.log.Logger.Infof("not equal peer config %v", cfg)
	}
}

// OSからパケットを受信して、WireGuardに送信する
func (w *Wrapper) Read(buffs [][]byte, sizes []int, offset int) (n int, err error) {
	if !w.started.Load() {
		<-w.startCh
	}

	// netstackのinject()のInjectOutboundPacketBufferから呼ばれる
	// or
	// poolVector経由で未消費のパケットを消費する、もう一度ReadするためにsendBufferConsumed()->pollVector().bufferConsumed channel->vectorOutboundで呼ばれる
	res, ok := <-w.vectorOutbound
	if !ok {
		return 0, io.EOF
	}

	if res.err != nil && len(res.data) == 0 {
		return 0, res.err
	}

	if res.data == nil {
		w.log.Logger.Infof("vector outbound data is nil")
		return 0, err
	}

	// todo (snt)
	// add filter

	var buffsPos int
	var copyBuffer []byte

	for _, data := range res.data {
		// TODO(snt) Readメソッドがバグったらこれも試してみる
		// a := data[res.dataOffset:]
		// n := copy(buffs[buffsPos][offset:], a)
		n := copy(buffs[buffsPos][offset:], copyBuffer)
		if n != len(data)-res.dataOffset {
			panic(fmt.Sprintf("short copy: %d != %d", n, len(data)-res.dataOffset))
		}
		sizes[buffsPos] = n
		buffsPos++
	}

	// 未消費パケットの対応
	if &res.data[0] == &w.vectorBuffer[0] {
		// pollVectorで待機している、bufferConsumedにもう一度チャネルを送信して
		// 再度Readを実行する
		w.sendBufferConsumed()
	}

	return buffsPos, nil
}

func (t *Wrapper) sendBufferConsumed() {
	t.bufferConsumedMu.Lock()
	defer t.bufferConsumedMu.Unlock()
	if t.bufferConsumedClosed {
		return
	}
	t.bufferConsumed <- struct{}{}
}

func (w *Wrapper) BatchSize() int {
	return w.tundev.BatchSize()
}

// netstack経由でInjectInboundPacketBufferが呼ばれWriteが呼ばれる
// Writeは着信したパケットを受けとる
func (w *Wrapper) Write(bufs [][]byte, offset int) (int, error) {
	// TODO(snt)
	// add filter
	// incomingのrule

	if len(bufs) > 0 {
		_, err := w.tdevWrite(bufs, offset)
		return len(bufs), err
	}
	return 0, nil
}

func (t *Wrapper) tdevWrite(buffs [][]byte, offset int) (int, error) {
	return t.tundev.Write(buffs, offset)
}

func (w *Wrapper) pumpEvents() {
	defer close(w.eventsUpDown)
	defer close(w.eventsOther)
	src := w.tundev.Events()
	for {
		var event tun.Event
		var ok bool
		select {
		case <-w.closed:
			return
		case event, ok = <-src:
			if !ok {
				return
			}
		}

		dst := w.eventsOther
		if event&(tun.EventUp|tun.EventDown) != 0 {
			dst = w.eventsUpDown
		}
		select {
		case <-w.closed:
			return
		case dst <- event:
		}
	}
}

func (t *Wrapper) isClosed() bool {
	select {
	case <-t.closed:
		return true
	default:
		return false
	}
}

func (t *Wrapper) InjectInboundCopy(packet []byte) error {
	// We duplicate this check from InjectInboundDirect here
	// to avoid wasting an allocation on an oversized packet.
	if len(packet) > MaxPacketSize {
		return errors.New("packet too big")
	}
	if len(packet) == 0 {
		return nil
	}

	buf := make([]byte, PacketStartOffset+len(packet))
	copy(buf[PacketStartOffset:], packet)

	return t.InjectInboundDirect(buf, PacketStartOffset)
}

// netstackから呼ばれる、vectorOutboundにチャネルを送信して、Readメソッドに送信します
func (t *Wrapper) InjectOutbound(pkt []byte) error {
	if len(pkt) > device.MaxContentSize {
		return errors.New("packet too big")
	}
	if len(pkt) == 0 {
		return nil
	}
	t.injectOutbound(tunInjectedRead{data: pkt})
	return nil
}

func (w *Wrapper) injectOutbound(r tunInjectedRead) {
	w.outboundMu.Lock()
	defer w.outboundMu.Unlock()
	if w.outboundClosed {
		return
	}
	w.vectorOutbound <- tunVectorReadResult{
		injected: r,
	}
}

const PacketStartOffset = device.MessageTransportHeaderSize

// netstackのjnject()から使用される
// パケットを送信するときに呼ばれる
func (t *Wrapper) InjectInboundPacketBuffer(pkt *stack.PacketBuffer) error {
	buf := make([]byte, PacketStartOffset+pkt.Size())

	n := copy(buf[PacketStartOffset:], pkt.NetworkHeader().Slice())
	n += copy(buf[PacketStartOffset+n:], pkt.TransportHeader().Slice())
	n += copy(buf[PacketStartOffset+n:], pkt.Data().AsRange().ToSlice())
	if n != pkt.Size() {
		panic("unexpected packet size after copy")
	}
	pkt.DecRef()

	return t.InjectInboundDirect(buf, PacketStartOffset)
}

func (w *Wrapper) InjectInboundDirect(buf []byte, offset int) error {
	if len(buf) > device.MaxContentSize {
		return errors.New("packet too big")
	}
	if len(buf) < offset {
		return errors.New("packet too big")
	}
	if offset < device.MessageTransportHeaderSize {
		return errors.New("packet too small")
	}

	_, err := w.tdevWrite([][]byte{buf}, offset)
	return err
}

// netstackのjnject()から使用される
// パケットを読む時に呼ばれる
func (w *Wrapper) InjectOutboundPacketBuffer(pkt *stack.PacketBuffer) error {
	size := pkt.Size()
	if size > device.MaxContentSize {
		pkt.DecRef()
		return errors.New("packet too big")
	}
	if size == 0 {
		pkt.DecRef()
		return nil
	}

	w.injectOutbound(tunInjectedRead{packet: pkt})
	return nil
}

func (w *Wrapper) EventsUpDown() chan tun.Event {
	return w.eventsUpDown
}

func (w *Wrapper) Events() <-chan tun.Event {
	return w.eventsOther
}

func (w *Wrapper) File() *os.File {
	return w.tundev.File()
}

func (w *Wrapper) MTU() (int, error) {
	return w.tundev.MTU()
}

func (w *Wrapper) Name() (string, error) {
	return w.tundev.Name()
}

func (w *Wrapper) Close() error {
	var err error
	w.allClose.Do(func() {
		close(w.startCh)
		close(w.closed)

		// safety consumed close
		w.bufferConsumedMu.Lock()
		w.bufferConsumedClosed = true
		close(w.bufferConsumed)
		w.bufferConsumedMu.Unlock()

		// safety outbound close
		w.outboundMu.Lock()
		w.outboundClosed = true
		close(w.vectorOutbound)
		w.outboundMu.Unlock()

		close(w.bufferConsumed)
		err = w.tundev.Close()
	})
	return err
}

func (t *Wrapper) Unwrap() tun.Device {
	return t.tundev
}
