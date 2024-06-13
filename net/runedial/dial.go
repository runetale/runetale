package runedial

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/runetale/runetale/log"
)

// dialer
// netstackからDialerを通じて、tundev wrapperのwireguardと通信を行う
type Dialer struct {
	Logger *log.Logger

	UseNetstackForIP func(netip.Addr) bool
	// netstackのNetstackDialTCPがセットされる, netstack経由でwireguardと通信ができるようになる
	NetstackDialTCP func(context.Context, netip.AddrPort) (net.Conn, error)
	// netstackのDialContextUDPがセットされる, netstack経由でwireguardと通信ができるようになる
	NetstackDialUDP func(context.Context, netip.AddrPort) (net.Conn, error)

	tunName string
	mu      sync.Mutex
}

func (d *Dialer) SetTUNName(name string) {
	d.mu.Lock()
	d.tunName = name
	defer d.mu.Unlock()
}

func (d *Dialer) GetTUNName() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tunName
}
