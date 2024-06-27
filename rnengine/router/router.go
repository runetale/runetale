package router

import (
	"net/netip"

	"github.com/runetale/runetale/log"
	"golang.zx2c4.com/wireguard/tun"
)

type Router interface {
	Up() error
	Set(*Config) error
	Close() error
}

func New(tundev tun.Device, log *log.Logger) (Router, error) {
	return newUserspaceRouter(tundev, log)
}

func CleanUp(log *log.Logger, tunName string) {
	cleanUp(tunName, log)
}

type Config struct {
	// これがこのNodeのアドレス
	LocalAddrs []netip.Prefix

	// advertisingするRoutes
	Routes []netip.Prefix

	// Runetale経由でroutingされるべきでないルート
	LocalRoutes []netip.Prefix

	// only linux?
	IsSNATSubnetRoutes  bool
	IsStatefulFiltering bool
	NetfilterKind       string
}
