package wgconfig

import (
	"io"
	"sort"

	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/utils/multierr"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

func NewDevice(tunDev tun.Device, bind conn.Bind, logger *device.Logger) *device.Device {
	dev := device.NewDevice(tunDev, bind, logger)
	dev.DisableSomeRoamingForBrokenMobileSemantics()
	return dev
}

func DeviceConfig(d *device.Device) (*WgConfig, error) {
	r, w := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		errc <- d.IpcGetOperation(w)
		w.Close()
	}()
	cfg, fromErr := FromUAPI(r)
	r.Close()
	getErr := <-errc
	err := multierr.New(getErr, fromErr)
	if err != nil {
		return nil, err
	}
	sort.Slice(cfg.Peers, func(i, j int) bool {
		return cfg.Peers[i].PublicKey.Less(cfg.Peers[j].PublicKey)
	})
	return cfg, nil
}

func ReconfigDevice(d *device.Device, cfg *WgConfig, logger *log.Logger) (err error) {
	defer func() {
		if err != nil {
			logger.Logger.Infof("wgcfg.Reconfig failed: %v", err)
		}
	}()

	prev, err := DeviceConfig(d)
	if err != nil {
		return err
	}

	r, w := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		errc <- d.IpcSetOperation(r)
		r.Close()
	}()

	toErr := cfg.ToUAPI(logger, w, prev)
	w.Close()
	setErr := <-errc
	return multierr.New(setErr, toErr)
}
