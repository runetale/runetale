package ipproto

import (
	"fmt"
	"strconv"

	"unicode"
	"unicode/utf8"
)

const stackArraySize = 32

// Get is equivalent to:
//
//	v := m[strings.ToLower(k)]
func Get[K ~string, V any](m map[K]V, k K) V {
	if isLowerASCII(string(k)) {
		return m[k]
	}
	var a [stackArraySize]byte
	return m[K(appendToLower(a[:0], string(k)))]
}

// GetOk is equivalent to:
//
//	v, ok := m[strings.ToLower(k)]
func GetOk[K ~string, V any](m map[K]V, k K) (V, bool) {
	if isLowerASCII(string(k)) {
		v, ok := m[k]
		return v, ok
	}
	var a [stackArraySize]byte
	v, ok := m[K(appendToLower(a[:0], string(k)))]
	return v, ok
}

// Set is equivalent to:
//
//	m[strings.ToLower(k)] = v
func Set[K ~string, V any](m map[K]V, k K, v V) {
	if isLowerASCII(string(k)) {
		m[k] = v
		return
	}
	// TODO(https://go.dev/issues/55930): This currently always allocates.
	// An optimization to the compiler and runtime could make this allocate-free
	// in the event that we are overwriting a map entry.
	//
	// Alternatively, we could use string interning.
	// See an example intern data structure, see:
	//	https://github.com/go-json-experiment/json/blob/master/intern.go
	var a [stackArraySize]byte
	m[K(appendToLower(a[:0], string(k)))] = v
}

// Delete is equivalent to:
//
//	delete(m, strings.ToLower(k))
func Delete[K ~string, V any](m map[K]V, k K) {
	if isLowerASCII(string(k)) {
		delete(m, k)
		return
	}
	var a [stackArraySize]byte
	delete(m, K(appendToLower(a[:0], string(k))))
}

// AppendSliceElem is equivalent to:
//
//	append(m[strings.ToLower(k)], v)
func AppendSliceElem[K ~string, S []E, E any](m map[K]S, k K, vs ...E) {
	// if the key is already lowercased
	if isLowerASCII(string(k)) {
		m[k] = append(m[k], vs...)
		return
	}

	// if key needs to become lowercase, uses appendToLower
	var a [stackArraySize]byte
	s := appendToLower(a[:0], string(k))
	m[K(s)] = append(m[K(s)], vs...)
}

func isLowerASCII(s string) bool {
	for i := range len(s) {
		if c := s[i]; c >= utf8.RuneSelf || ('A' <= c && c <= 'Z') {
			return false
		}
	}
	return true
}

func appendToLower(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case 'A' <= c && c <= 'Z':
			b = append(b, c+('a'-'A'))
		case c < utf8.RuneSelf:
			b = append(b, c)
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			b = utf8.AppendRune(b, unicode.ToLower(r))
			i += n - 1 // -1 to compensate for i++ in loop advancement
		}
	}
	return b
}

// Version describes the IP address version.
type Version uint8

// Valid Version values.
const (
	Version4 = 4
	Version6 = 6
)

func (p Version) String() string {
	switch p {
	case Version4:
		return "IPv4"
	case Version6:
		return "IPv6"
	default:
		return fmt.Sprintf("Version-%d", int(p))
	}
}

// Proto is an IP subprotocol as defined by the IANA protocol
// numbers list
// (https://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml),
// or the special values Unknown or Fragment.
type Proto uint8

const (
	// Unknown represents an unknown or unsupported protocol; it's
	// deliberately the zero value. Strictly speaking the zero
	// value is IPv6 hop-by-hop extensions, but we don't support
	// those, so this is still technically correct.
	Unknown Proto = 0x00

	// Values from the IANA registry.
	ICMPv4 Proto = 0x01
	IGMP   Proto = 0x02
	ICMPv6 Proto = 0x3a
	TCP    Proto = 0x06
	UDP    Proto = 0x11
	DCCP   Proto = 0x21
	GRE    Proto = 0x2f
	SCTP   Proto = 0x84

	// Fragment represents any non-first IP fragment, for which we
	// don't have the sub-protocol header (and therefore can't
	// figure out what the sub-protocol is).
	//
	// 0xFF is reserved in the IANA registry, so we steal it for
	// internal use.
	Fragment Proto = 0xFF
)

// Deprecated: use MarshalText instead.
func (p Proto) String() string {
	switch p {
	case Unknown:
		return "Unknown"
	case Fragment:
		return "Frag"
	case ICMPv4:
		return "ICMPv4"
	case IGMP:
		return "IGMP"
	case ICMPv6:
		return "ICMPv6"
	case UDP:
		return "UDP"
	case TCP:
		return "TCP"
	case SCTP:
		return "SCTP"
	case GRE:
		return "GRE"
	case DCCP:
		return "DCCP"
	default:
		return fmt.Sprintf("IPProto-%d", int(p))
	}
}

// Prefer names from
// https://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml
// unless otherwise noted.
var (
	// preferredNames is the set of protocol names that re produced by
	// MarshalText, and are the preferred representation.
	preferredNames = map[Proto]string{
		51:     "ah",
		DCCP:   "dccp",
		8:      "egp",
		50:     "esp",
		47:     "gre",
		ICMPv4: "icmp",
		IGMP:   "igmp",
		9:      "igp",
		4:      "ipv4",
		ICMPv6: "ipv6-icmp",
		SCTP:   "sctp",
		TCP:    "tcp",
		UDP:    "udp",
	}

	// acceptedNames is the set of protocol names that are accepted by
	// UnmarshalText.
	acceptedNames = map[string]Proto{
		"ah":        51,
		"dccp":      DCCP,
		"egp":       8,
		"esp":       50,
		"gre":       47,
		"icmp":      ICMPv4,
		"icmpv4":    ICMPv4,
		"icmpv6":    ICMPv6,
		"igmp":      IGMP,
		"igp":       9,
		"ip-in-ip":  4, // IANA says "ipv4"; Wikipedia/popular use says "ip-in-ip"
		"ipv4":      4,
		"ipv6-icmp": ICMPv6,
		"sctp":      SCTP,
		"tcp":       TCP,
		"udp":       UDP,
	}
)

// UnmarshalText implements encoding.TextUnmarshaler. If the input is empty, p
// is set to 0. If an error occurs, p is unchanged.
func (p *Proto) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*p = 0
		return nil
	}

	if u, err := strconv.ParseUint(string(b), 10, 8); err == nil {
		*p = Proto(u)
		return nil
	}

	if newP, ok := GetOk(acceptedNames, string(b)); ok {
		*p = newP
		return nil
	}

	return fmt.Errorf("proto name %q not known; use protocol number 0-255", b)
}

// MarshalText implements encoding.TextMarshaler.
func (p Proto) MarshalText() ([]byte, error) {
	if s, ok := preferredNames[p]; ok {
		return []byte(s), nil
	}
	return []byte(strconv.Itoa(int(p))), nil
}

// MarshalJSON implements json.Marshaler.
func (p Proto) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(p))), nil
}

// UnmarshalJSON implements json.Unmarshaler. If the input is empty, p is set to
// 0. If an error occurs, p is unchanged. The input must be a JSON number or an
// accepted string name.
func (p *Proto) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		*p = 0
		return nil
	}
	if b[0] == '"' {
		b = b[1 : len(b)-1]
	}
	return p.UnmarshalText(b)
}
