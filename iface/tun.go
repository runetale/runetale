package iface

import (
	"fmt"
	"net"
)

// Avoiding conflicts
// The lower byte (bits 0-7) is often used by system administrators or other common network configuration tools (e.g., Kubernetes), so these bits are avoided.
// The next 8 bits (bits 8-15) are known to be used frequently for specific purposes (e.g., some bits by Kubernetes), so these are also avoided.
// Based on actual usage examples and documentation, fwmark is often represented within 16 bits (bits 0-15). Therefore, the upper 16 bits (bits 16-31) are presumed to be relatively unused.
// It was decided to use the 16th to 23rd bits (0xFF0000) for fwmark. This range was chosen to avoid conflicts with existing systems and applications.

// 31-24 left for the system administrator to use.
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
