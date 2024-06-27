package router

import (
	"github.com/runetale/runetale/log"
	"golang.zx2c4.com/wireguard/tun"
)

func newUserspaceRouter(tundev tun.Device, log *log.Logger) (Router, error) {
	return newUserspaceBSDRouter(tundev, log)
}

func cleanUp(tunname string, log *log.Logger) {
}
