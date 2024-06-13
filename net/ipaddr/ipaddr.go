package ipaddr

import "net/netip"

func IPv4(a, b, c, d uint8) netip.Addr {
	return netip.AddrFrom4([4]byte{a, b, c, d})
}

func Unmap(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
