package ports

import (
	"errors"
	"net"

	"github.com/runetale/runetale/wg"
)

func GetFreePort(start int) (uint16, error) {
	addr := net.UDPAddr{}
	if start == 0 {
		start = wg.DefaultWgPort
	}
	for x := start; x <= 65535; x++ {
		addr.Port = x
		conn, err := net.ListenUDP("udp", &addr)
		if err != nil {
			continue
		}
		conn.Close()
		return uint16(x), nil
	}
	return 0, errors.New("no free ports")
}
