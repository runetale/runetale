package dial

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/runetale/runetale/log"
)

type Dialer struct {
	Logger *log.Logger

	UseNetstackForIP func(netip.Addr) bool
	NetstackDialTCP  func(context.Context, netip.AddrPort) (net.Conn, error)
	NetstackDialUDP  func(context.Context, netip.AddrPort) (net.Conn, error)

	tunName string
	mu      sync.Mutex
}

func (d *Dialer) SetTUNName(name string) {
	d.mu.Lock()
	d.tunName = name
	defer d.mu.Unlock()
}
