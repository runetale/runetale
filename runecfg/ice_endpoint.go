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

func (e EndpointType) IsLocal() bool {
	switch e {
	case EndpointLocal:
		return true
	case EndpointSTUN:
		return false
	}
	return true
}

type Endpoint struct {
	// iceのremoteconnをparseして取得したAddressです
	Addr netip.AddrPort
	Type EndpointType
}
