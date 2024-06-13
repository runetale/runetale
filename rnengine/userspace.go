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
	"github.com/runetale/runetale/net/packet"
	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/net/rntun/filter"
	"github.com/runetale/runetale/net/runedial"
	"github.com/runetale/runetale/rnengine/router"
	"github.com/runetale/runetale/rnengine/wgconfig"
	"github.com/runetale/runetale/rnengine/wonderwall"
	"github.com/runetale/runetale/rnengine/wonderwall/stdnet"
	"github.com/runetale/runetale/runecfg"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

type Config struct {
	Tun    tun.Device
	IsTAP  bool
	Dialer *runedial.Dialer
	Router router.Router

	WgListenPort   uint16
	IfaceBlackList []string
	NodeKey        string

	SignalClient grpc_client.SignalClientImpl
	ServerClient grpc_client.ServerClientImpl
	ClientConfig *conf.ClientConfig

	IsDebug bool
}

type userspaceEngine struct {
	logger *log.Logger

	tundev    *rntun.Wrapper
	wgdev     *device.Device
	mu        sync.Mutex
	waitCh    chan struct{}
	wgdevLock sync.Mutex

	wgListenPort uint16

	router router.Router

	wonderConn *wonderwall.Conn

	// trueになるまで、handleLocalPacketを開始しない
	isLocalAddr atomics.AtomicValue[func(netip.Addr) bool]

	// wonderwall->iceからのcallback関数でendpointsはセットされる
	// iceから取得したpeer毎のremoteconn
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

func NewUserspaceEngine(log *log.Logger, config Config) (_ Engine, reterr error) {
	var closePool closeOnErrorPool
	defer closePool.closeAllIfError(&reterr)

	if config.Tun == nil {
		config.Tun = rntun.NewKagemusha()
	}

	// tun deviceのwrapperを初期化
	var tunDev *rntun.Wrapper
	tunDev = rntun.NewWrapper(log, config.Tun)
	closePool.add(tunDev)

	// userspace engineを初期化
	e := newUserspaceEngine(log, config, tunDev)
	e.isLocalAddr.Store(func(ip netip.Addr) bool { return false })

	tunName, _ := config.Tun.Name()
	config.Dialer.SetTUNName(tunName)

	// todo (snt)
	// macosとiosに依存する処理
	// isLocalAddressがSetされたら、この関数が動き出す
	e.tundev.ReadFilterPacketOutboundToUserspaceEngineIntercept = e.handleLocalPackets

	wonderConn := wonderwall.NewConn(
		config.SignalClient,
		config.ServerClient,
		config.ClientConfig,
		config.NodeKey,
		config.WgListenPort,
		log,
		config.IsDebug,
	)
	e.wonderConn = wonderConn
	closePool.add(wonderConn)

	// wonderwallでremote connのendpointが更新された時のコールバック関数をセット
	updateEndpointFn := func(endpoints runecfg.Endpoint) {
		e.mu.Lock()
		e.endpoints = append(e.endpoints, endpoints)
		e.mu.Unlock()
	}
	wonderConn.SetUpdateEndpointFn(updateEndpointFn)

	// todo (snt) ice bindをしっかりと見る
	transportNet, err := stdnet.NewNet(config.IfaceBlackList)
	if err != nil {
		log.Logger.Errorf("failed to NewNet %s", err.Error())
	}
	iceBind := wonderwall.NewICEBind(transportNet, log)

	e.wgdev = wgconfig.NewDevice(e.tundev, iceBind, device.NewLogger(device.LogLevelSilent, "runetale: "))
	closePool.addFunc(e.wgdev.Close)

	go wonderConn.Start()

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
		return nil, fmt.Errorf("wgdev.Up: %w", err)
	}

	log.Logger.Infof("Bringing Router up...")
	if err := e.router.Up(); err != nil {
		return nil, fmt.Errorf("router.Up: %w", err)
	}

	return e, nil
}

func (e *userspaceEngine) handleLocalPackets(p *packet.Parsed, w *rntun.Wrapper) filter.Response {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		_, ok := e.isLocalAddr.LoadOk()
		if !ok {
			e.logger.Logger.Infof("local address was nil, not set by router")
		}
		// todo (snt)
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
	e.wonderConn.Close()
	close(e.waitCh)
}

func (e *userspaceEngine) Reconfig(wgConfig *wgconfig.WgConfig, routerConfig *router.Config) error {
	e.wgdevLock.Lock()
	defer e.wgdevLock.Lock()

	// todo (snt) routerConfig.LocalAddrsをStoreする
	e.isLocalAddr.Store(func(ip netip.Addr) bool { return true })

	// tundevのpeertableを設定する
	// ここで渡されたwgConfigはpeerConifgに設定され、
	// - InjectInboundPacketBuffer() "netstackからデバイス経由でパケットを送信するとき"
	// - injectedRead(res tunInjectedRead, buf []byte, offset int) (int, error) "Readでデバイスからパケットを受信するとき"
	// - Write(buffs [][]byte, offset int) (int, error) "Writeでデバイスからパケットを送信するとき"
	// の時にpeerConfig.Load()を使って読み込むことで、最新のpeerの情報でフィルターをかけている
	e.tundev.SetWGConfig(wgConfig)

	// syncRemoteNodesで取得した値をWgConfigに書き換えて
	// WireGuardの設定を更新
	err := wgconfig.ReconfigDevice(e.wgdev, wgConfig, e.logger)
	if err != nil {
		return err
	}

	// このnodeのip アドレスのアサインやsubnetのルーティングの設定を行う
	err = e.router.Set(routerConfig)
	if err != nil {
		return err
	}

	// wonderwallのpeerconnsが持つiceのmapに対して
	// 必要であればlocal proxyを開始するようにする
	err = e.wonderConn.UpdateConfig(wgConfig)
	if err != nil {
		return err
	}

	return nil
}
