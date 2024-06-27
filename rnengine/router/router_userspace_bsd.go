//go:build darwin || freebsd

package router

import (
	"net/netip"
	"os/exec"
	"runtime"

	"github.com/runetale/runetale/log"

	defaultLog "log"

	"golang.zx2c4.com/wireguard/tun"
)

type userspaceBSDRouter struct {
	tunname string
	routes  map[netip.Prefix]bool // advertisingするRoutes
	local   []netip.Prefix        // Runetale経由でroutingされるべきでないルート
	log     *log.Logger
}

func newUserspaceBSDRouter(tundev tun.Device, log *log.Logger) (Router, error) {
	tname, err := tundev.Name()
	if err != nil {
		return nil, err
	}

	return &userspaceBSDRouter{
		tunname: tname,
		log:     log,
	}, nil
}

func inet(p netip.Prefix) string {
	if p.Addr().Is6() {
		return "inet6"
	}
	return "inet"
}

func cmd(args ...string) *exec.Cmd {
	if len(args) == 0 {
		defaultLog.Fatalf("exec.Cmd(%#v) invalid; need argv[0]", args)
	}
	return exec.Command(args[0], args[1:]...)
}

func (r *userspaceBSDRouter) Up() error {
	ifup := []string{"ifconfig", r.tunname, "up"}
	if out, err := cmd(ifup...).CombinedOutput(); err != nil {
		r.log.Logger.Infof("running ifconfig failed: %v\n%s", err, out)
		return err
	}
	return nil
}

func (r *userspaceBSDRouter) Set(config *Config) error {
	if config == nil {
		r.log.Logger.Errorf("router config setting is empty")
	}

	addrsToRemove := r.addrsToRemove(config.LocalAddrs)

	// Set own ip address to tun device
	for _, addr := range addrsToRemove {
		arg := []string{"ifconfig", r.tunname, inet(addr), addr.String(), "-alias"}
		out, err := cmd(arg...).CombinedOutput()
		if err != nil {
			r.log.Logger.Errorf("addr del failed: %v => %v\n%s", arg, err, out)
			return err
		}
	}
	for _, addr := range r.addrsToAdd(config.LocalAddrs) {
		var arg []string
		if runtime.GOOS == "freebsd" && addr.Addr().Is6() && addr.Bits() == 128 {
			tmp := netip.PrefixFrom(addr.Addr(), 48)
			arg = []string{"ifconfig", r.tunname, inet(tmp), tmp.String()}
		} else {
			arg = []string{"ifconfig", r.tunname, inet(addr), addr.String(), addr.Addr().String()}
		}
		out, err := cmd(arg...).CombinedOutput()
		if err != nil {
			r.log.Logger.Errorf("addr add failed: %v => %v\n%s", arg, err, out)
			return err
		}
	}

	r.local = append([]netip.Prefix{}, config.LocalAddrs...)

	return nil
}

func (r *userspaceBSDRouter) Close() error {
	return nil
}

func (r *userspaceBSDRouter) addrsToAdd(newLocalAddrs []netip.Prefix) (add []netip.Prefix) {
	for _, cur := range newLocalAddrs {
		found := false
		for _, v := range r.local {
			found = (v == cur)
			if found {
				break
			}
		}
		if !found {
			add = append(add, cur)
		}
	}
	return
}

// localに割り当てられるaddressがないか確認する
func (r *userspaceBSDRouter) addrsToRemove(newLocalAddrs []netip.Prefix) (remove []netip.Prefix) {
	for _, cur := range r.local {
		found := false
		for _, v := range newLocalAddrs {
			found = (v == cur)
			if found {
				break
			}
		}
		if !found {
			remove = append(remove, cur)
		}
	}
	return
}
