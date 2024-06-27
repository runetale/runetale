// tun.DeviceのWrapper
// userspaceのネットワークスタックでパケットをフィルタリングする

package rntun

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/net/rntun/filter"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type FilterFunc func() filter.Response

type Wrapper struct {
	tundev tun.Device
	isTAP  bool

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

	// TODO(snt)
	// add filter func
	PreFilterPacketOutboundToWireGuardEngineIntercept FilterFunc
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

// TODO(snt)
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

func (t *Wrapper) sendVectorOutboundFromPoll(r tunVectorReadResult) {
	t.outboundMu.Lock()
	defer t.outboundMu.Unlock()
	if t.outboundClosed {
		return
	}
	t.vectorOutbound <- r
}

// poolVector経由でReadが呼ばれる
// デバイスからパケットを受信
func (w *Wrapper) Read(buffs [][]byte, sizes []int, offset int) (n int, err error) {
	if !w.started.Load() {
		<-w.startCh
	}

	// netstack、もしくはpoolVectorから送られてくる
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

	// TODO(snt)
	// add filter
	// outgoingのrule
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
// デバイスからパケットを送信
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
