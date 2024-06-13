package rtaddr

import (
	"net/netip"
	"sync"

	"github.com/runetale/runetale/net/ipaddr"
)

var (
	cgnatRange oncePrefix
)

type oncePrefix struct {
	sync.Once
	v netip.Prefix
}

// Local DNS ServerやProxy ServerのIP空間
func RunetaleServiceIP() netip.Addr {
	return ipaddr.IPv4(100, 100, 100, 100)
}

func FindRunetaleIPv4(addrs []netip.Prefix) netip.Addr {
	for _, ap := range addrs {
		a := ap.Addr()
		if a.Is4() && IsRunetaleIP(a) {
			return a
		}
	}
	return netip.Addr{}
}

func IsRunetaleIP(ip netip.Addr) bool {
	if ip.Is4() {
		return CGNATRange().Contains(ip)
	}
	return false
}

func CGNATRange() netip.Prefix {
	cgnatRange.Do(func() { checkIPPrefix(&cgnatRange.v, "100.64.0.0/10") })
	return cgnatRange.v
}

func checkIPPrefix(v *netip.Prefix, prefix string) {
	var err error
	*v, err = netip.ParsePrefix(prefix)
	if err != nil {
		panic(err)
	}
}
