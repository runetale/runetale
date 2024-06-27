package runecfg

import "net/netip"

type EndpointType int

const (
	EndpointLocal = EndpointType(0)
	EndpointSTUN  = EndpointType(1)
)

func (e EndpointType) String() string {
	switch e {
	case EndpointLocal:
		return "local"
	case EndpointSTUN:
		return "stun"
	}
	return "none"
}

type Endpoint struct {
	Addr netip.AddrPort
	Type EndpointType
}
