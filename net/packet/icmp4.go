package packet

import (
	"encoding/binary"

	"github.com/runetale/runetale/types/ipproto"
)

const icmp4HeaderLength = 4

type ICMP4Type uint8

const (
	ICMP4EchoReply    ICMP4Type = 0x00
	ICMP4EchoRequest  ICMP4Type = 0x08
	ICMP4Unreachable  ICMP4Type = 0x03
	ICMP4TimeExceeded ICMP4Type = 0x0b
	ICMP4ParamProblem ICMP4Type = 0x12
)

func (t ICMP4Type) String() string {
	switch t {
	case ICMP4EchoReply:
		return "EchoReply"
	case ICMP4EchoRequest:
		return "EchoRequest"
	case ICMP4Unreachable:
		return "Unreachable"
	case ICMP4TimeExceeded:
		return "TimeExceeded"
	case ICMP4ParamProblem:
		return "ParamProblem"
	default:
		return "Unknown"
	}
}

// https://www.iana.org/assignments/icmp-parameters/icmp-parameters.xhtml
type ICMP4Code uint8

const (
	ICMP4NoCode ICMP4Code = 0
)

type ICMP4Header struct {
	IP4Header
	Type ICMP4Type
	Code ICMP4Code
}

func (h ICMP4Header) Len() int {
	return h.IP4Header.Len() + icmp4HeaderLength
}

func (h ICMP4Header) Marshal(buf []byte) error {
	if len(buf) < h.Len() {
		return errSmallBuffer
	}
	if len(buf) > maxPacketLength {
		return errLargePacket
	}
	// The caller does not need to set this.
	h.IPProto = ipproto.ICMPv4

	buf[20] = uint8(h.Type)
	buf[21] = uint8(h.Code)

	h.IP4Header.Marshal(buf)

	binary.BigEndian.PutUint16(buf[22:24], ip4Checksum(buf))

	return nil
}

func (h *ICMP4Header) ToResponse() {
	h.Type = ICMP4EchoReply
	h.Code = ICMP4NoCode
	h.IP4Header.ToResponse()
}
