package packet

import (
	"errors"
	"math"
)

const tcpHeaderLength = 20
const sctpHeaderLength = 12

const maxPacketLength = math.MaxUint16

var (
	errSmallBuffer = errors.New("buffer too small")
	errLargePacket = errors.New("packet too large")
)

type Header interface {
	Len() int
	Marshal(buf []byte) error
}

type HeaderChecksummer interface {
	Header

	WriteChecksum(buf []byte)
}

func Generate(h Header, payload []byte) []byte {
	hlen := h.Len()
	buf := make([]byte, hlen+len(payload))

	copy(buf[hlen:], payload)
	h.Marshal(buf)

	if hc, ok := h.(HeaderChecksummer); ok {
		hc.WriteChecksum(buf)
	}

	return buf
}
