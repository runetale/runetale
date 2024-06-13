package packet

import (
	"encoding/binary"

	"github.com/runetale/runetale/types/ipproto"
)

// fix
const udpHeaderLength = 8

type UDP4Header struct {
	IP4Header
	SrcPort uint16
	DstPort uint16
}

func (h UDP4Header) Len() int {
	return h.IP4Header.Len() + udpHeaderLength
}

func (h UDP4Header) Marshal(buf []byte) error {
	if len(buf) < h.Len() {
		return errSmallBuffer
	}
	if len(buf) > maxPacketLength {
		return errLargePacket
	}

	h.IPProto = ipproto.UDP

	length := len(buf) - h.IP4Header.Len()
	binary.BigEndian.PutUint16(buf[20:22], h.SrcPort)
	binary.BigEndian.PutUint16(buf[22:24], h.DstPort)
	binary.BigEndian.PutUint16(buf[24:26], uint16(length))
	binary.BigEndian.PutUint16(buf[26:28], 0) // blank checksum

	h.IP4Header.marshalPseudo(buf)
	binary.BigEndian.PutUint16(buf[26:28], ip4Checksum(buf[ip4PseudoHeaderOffset:]))

	h.IP4Header.Marshal(buf)

	return nil
}

func (h *UDP4Header) ToResponse() {
	h.SrcPort, h.DstPort = h.DstPort, h.SrcPort
	h.IP4Header.ToResponse()
}
