package iface

import (
	"fmt"
	"net"
)

// | 31-24   | 23-16   | 15-8    | 7-0     |
// | 00000000| 11111111| 00000000| 00000000|  (0xffffff00)

const (
	// | 23-16   | 15-8    | 7-0     |
	// | 11111111| 00000000| 00000000|
	RunetaleFwmarkMask = 0xff0000
)

// WGAddress Wireguard parsed address
type WGAddress struct {
	IP      net.IP
	Network *net.IPNet
}

// parseWGAddress parse a string ("1.2.3.4/24") address to WG Address
func parseWGAddress(address string) (WGAddress, error) {
	ip, network, err := net.ParseCIDR(address)
	if err != nil {
		return WGAddress{}, err
	}
	return WGAddress{
		IP:      ip,
		Network: network,
	}, nil
}

func (addr WGAddress) String() string {
	maskSize, _ := addr.Network.Mask.Size()
	return fmt.Sprintf("%s/%d", addr.IP.String(), maskSize)
}
