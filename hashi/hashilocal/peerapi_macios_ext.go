//go:build darwin || ios

package hashilocal

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ここも見直す
func init() {
	initListenConfig = initListenConfigNetworkExtension
}

// iOS/macOSでは、lc.Controlフックをsetsockoptに設定し、
// バインド先のインターフェース・インデックスを設定することで、ネットワーク・サンドボックスから抜け出せる
func initListenConfigNetworkExtension(nc *net.ListenConfig, ip netip.Addr, tunIf *net.Interface) error {
	return SetListenConfigInterfaceIndex(nc, tunIf.Index)
}

func SetListenConfigInterfaceIndex(lc *net.ListenConfig, ifIndex int) error {
	if lc == nil {
		return errors.New("nil ListenConfig")
	}
	if lc.Control != nil {
		return errors.New("ListenConfig.Control already set")
	}
	lc.Control = func(network, address string, c syscall.RawConn) error {
		return bindConnToInterface(c, network, address, ifIndex)
	}
	return nil
}

func bindConnToInterface(c syscall.RawConn, network, address string, ifIndex int) error {
	v6 := strings.Contains(address, "]:") || strings.HasSuffix(network, "6") // hacky test for v6
	proto := unix.IPPROTO_IP
	opt := unix.IP_BOUND_IF
	if v6 {
		proto = unix.IPPROTO_IPV6
		opt = unix.IPV6_BOUND_IF
	}

	var sockErr error
	err := c.Control(func(fd uintptr) {
		sockErr = unix.SetsockoptInt(int(fd), proto, opt, ifIndex)
	})
	if sockErr != nil {
		log.Panicf("[unexpected] netns: bindConnToInterface(%q, %q), v6=%v, index=%v: %v", network, address, v6, ifIndex, sockErr)
	}
	if err != nil {
		return fmt.Errorf("RawConn.Control on %T: %w", c, err)
	}
	return sockErr
}
