//go:build !linux

package rntun

import "golang.zx2c4.com/wireguard/tun"

func setLinkFeatures(dev tun.Device) error {
	return nil
}

func setLinkAttrs(iface tun.Device) error {
	return nil
}
