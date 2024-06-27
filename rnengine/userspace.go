package rnengine

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"runtime"
	"strings"
	"sync"

	"github.com/runetale/runetale/atomics"
	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/net/dial"
	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/net/rntun/filter"
	"github.com/runetale/runetale/rnengine/router"
	"github.com/runetale/runetale/rnengine/wonderwall"
	"github.com/runetale/runetale/runecfg"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

type Config struct {
	Tun          tun.Device
	IsTAP        bool
	Dialer       *dial.Dialer
	Router       router.Router
	WgListenPort uint16

	NodeKey      string
	SignalClient grpc_client.SignalClientImpl
	ServerClient grpc_client.ServerClientImpl
	Spec         *conf.Spec
	IsDebug      bool
}

type userspaceEngine struct {
	logger *log.Logger

	tundev *rntun.Wrapper
	wgdev  *device.Device
	mu     sync.Mutex
	waitCh chan struct{}

	wgListenPort uint16

	router router.Router

	// trueになるまで、handleLocalPacketを開始しない
	isLocalAddr atomics.AtomicValue[func(netip.Addr) bool]

	// wonderwallからのcallback関数でこの値はセットされる
	endpoints []runecfg.Endpoint

	closing bool
}

func newUserspaceEngine(log *log.Logger, conf Config, tundev *rntun.Wrapper) *userspaceEngine {
	e := &userspaceEngine{
		logger:       log,
		tundev:       tundev,
		wgListenPort: conf.WgListenPort,
		router:       conf.Router,
	}
	return e
}

func NewDevice(tunDev tun.Device, bind conn.Bind, logger *device.Logger) *device.Device {
	dev := device.NewDevice(tunDev, bind, logger)
	dev.DisableSomeRoamingForBrokenMobileSemantics()
	return dev
}

type closeOnErrorPool []func()

func (p *closeOnErrorPool) add(c io.Closer)   { *p = append(*p, func() { c.Close() }) }
func (p *closeOnErrorPool) addFunc(fn func()) { *p = append(*p, fn) }
func (p closeOnErrorPool) closeAllIfError(errp *error) {
	if *errp != nil {
		for _, closeFn := range p {
			closeFn()
		}
	}
}

func NewUserspaceEngine(log *log.Logger, config Config) (reterr error) {
	var closePool closeOnErrorPool
	defer closePool.closeAllIfError(&reterr)

	if config.Tun == nil {
		config.Tun = rntun.NewKagemusha()
	}

	// tun deviceのwrapperを設定
	var tunDev *rntun.Wrapper
	tunDev = rntun.NewWrapper(log, config.Tun)
	closePool.add(tunDev)

	// userspace engineを初期化
	e := newUserspaceEngine(log, config, tunDev)
	e.isLocalAddr.Store(func(ip netip.Addr) bool { return false })

	tunName, _ := config.Tun.Name()
	config.Dialer.SetTUNName(tunName)

	// これはmacosとiosに依存する処理
	// isLocalAddressがSetされたら、この関数が動き出す
	e.tundev.PreFilterPacketOutboundToWireGuardEngineIntercept = e.handleLocalPackets

	// wonderwall
	// wonderwallはserverとiceとsignale serverとのやり取りを行う
	// サーバーからremote nodeの情報を取得し、iceとsignalingを使用して、p2pに必要なremote conenctionを取得する
	// remote connectionはuserspace engineのendpointsにコールバック関数の結果として返ってくる

	ww := wonderwall.NewWonderwall(
		config.SignalClient,
		config.ServerClient,
		config.Spec,
		config.NodeKey,
		log,
		config.IsDebug,
	)

	updateEndpointFn := func(endpoints runecfg.Endpoint) {
		e.mu.Lock()
		e.endpoints = append(e.endpoints, endpoints)
		e.mu.Unlock()
	}

	ww.SetUpdateEndpointFn(updateEndpointFn)

	go ww.Start()

	// TODO(snt)
	// 6
	// wireguardのconn.Bindをwonderwallでoverrideする
	// e.wgdev = NewDevice(e.tundev, wonderwallconn.Bind(), device.NewLogger(device.LogLevelSilent, "runetale: "))
	e.wgdev = NewDevice(e.tundev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, "runetale: "))
	closePool.addFunc(e.wgdev.Close)

	go func() {
		up := false
		for event := range e.tundev.EventsUpDown() {
			if event&tun.EventUp != 0 && !up {
				log.Logger.Infof("TUNDEV UP EVENTS")
				up = true
			}
			if event&tun.EventDown != 0 && up {
				log.Logger.Infof("TUNDEV DOWN EVENTS")
				up = false
			}
		}
	}()

	go func() {
		select {
		case <-e.wgdev.Wait():
			e.mu.Lock()
			closing := e.closing
			e.mu.Unlock()
			if !closing {
				log.Logger.Infof("Closing the engine because the WireGuard device has been closed...")
				e.Close()
			}
		case <-e.waitCh:
			// continue
		}
	}()

	log.Logger.Infof("Bringing WireGuard device up...")
	if err := e.wgdev.Up(); err != nil {
		return fmt.Errorf("wgdev.Up: %w", err)
	}

	log.Logger.Infof("Bringing Router up...")
	if err := e.router.Up(); err != nil {
		return fmt.Errorf("router.Up: %w", err)
	}

	return nil
}

func (e *userspaceEngine) handleLocalPackets() filter.Response {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		_, ok := e.isLocalAddr.LoadOk()
		if !ok {
			e.logger.Logger.Infof("local address was nil, not set by router")
		}
		// TODO:(snt)
		// パースしたパケットのdstにlocalアドレス宛のパケットが含まれていることがある
		// それをdropするための処理があった方が適切である
	}
	return filter.Accept
}

// router, wgdev, tundevの順番にclose
func (e *userspaceEngine) Close() {
	e.mu.Lock()
	if e.closing {
		e.mu.Unlock()
		return
	}
	e.closing = true
	e.mu.Unlock()

	r := bufio.NewReader(strings.NewReader(""))
	e.wgdev.IpcSetOperation(r)

	e.router.Close()
	e.wgdev.Close()
	e.tundev.Close()
	close(e.waitCh)
}

// TODO(snt)
// Reconifg関数ではP2PをするためのWireGuardの設定とRouterの設定をコールバック関数でiceから取得する

// 1. tundevのWireGuardの設定を更新
// e.tundev.SetWGConfig(cfg)
// を使用してtundev(rntun.Wrapper)を更新する

// ここで設定されたcfgはpeerConifgに設定され、
// - InjectInboundPacketBuffer() netstackからデバイス経由でパケットを送信するとき
// - injectedRead(res tunInjectedRead, buf []byte, offset int) (int, error) Readでデバイスからパケットを受信するとき
// - Write(buffs [][]byte, offset int) (int, error) Writeでデバイスからパケットを送信するとき
// の時にLoadを使って読み込むことで、最新のpeerの情報で通信を行っている

// 2. wgdevのWireGuardの設定を更新
// if err := wgcfg.ReconfigDevice(e.wgdev, &min, e.logf); err != nil {
// を使用してwgdev(device.Device)を更新する

// 3. Routerの設定を更新
// err := e.router.Set(routerCfg)
// func (r *userspaceBSDRouter) Set(cfg *Config) (reterr error) {
// を使用してtunnameの名前を使って自身のIPとSubnetRoutingするIPを更新する
// linuxの場合はiptables or nftablesで更新

func Reconfig() {

}
