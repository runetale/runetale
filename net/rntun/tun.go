package rntun

import (
	"runtime"

	"github.com/runetale/runetale/log"

	"golang.zx2c4.com/wireguard/tun"
)

// todo: fix
var tunDiagnoseFailure func(tunName string, log *log.Logger, err error)

// todo: fix
func Diagnose(log *log.Logger, tunName string, err error) {
	if tunDiagnoseFailure != nil {
		tunDiagnoseFailure(tunName, log, err)
	} else {
		log.Logger.Infof("no TUN failure diagnostics for OS %q", runtime.GOOS)
	}
}

const (
	DEFAULTMTU uint32 = 1280
)

func New(log *log.Logger, tunName string) (tun.Device, string, error) {
	dev, err := tun.CreateTUN(tunName, int(DEFAULTMTU))
	if err != nil {
		return nil, "", nil
	}

	if err := setLinkFeatures(dev); err != nil {
		log.Logger.Infof("setting link features: %v", err)
	}
	if err := setLinkAttrs(dev); err != nil {
		log.Logger.Infof("setting link attributes: %v", err)
	}

	name, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, "", err
	}
	return dev, name, nil
}
