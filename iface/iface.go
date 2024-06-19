// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package iface

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/wg"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Iface struct {
	// your wireguard interface name
	Tun string
	// your wireguard private key
	WgPrivateKey string
	// your ip
	IP string
	// your cidr range
	CIDR string

	runelog *runelog.Runelog
}

func NewIface(
	tun, wgPrivateKey, ip string,
	cidr string, runelog *runelog.Runelog,
) *Iface {
	return &Iface{
		Tun:          tun,
		WgPrivateKey: wgPrivateKey,
		IP:           ip,
		CIDR:         cidr,

		runelog: runelog,
	}
}

func (i *Iface) ConfigureRemoteNodePeer(
	remoteNodePubKey, remoteip string,
	endpoint *net.UDPAddr,
	keepAlive time.Duration,
	preSharedKey string,
) error {
	i.runelog.Logger.Debugf(
		"configuring %s to remote node [%s:%s], remote endpoint [%s:%d]",
		i.Tun, remoteNodePubKey, remoteip, endpoint.IP.String(), endpoint.Port,
	)

	_, ipNet, err := net.ParseCIDR(remoteip)
	if err != nil {
		i.runelog.Logger.Errorf("failed to parse cidr")
		return err
	}

	i.runelog.Logger.Debugf("allowed remote ip [%s]", ipNet.IP.String())

	parsedRemoteNodePubKey, err := wgtypes.ParseKey(remoteNodePubKey)
	if err != nil {
		i.runelog.Logger.Errorf("failed to remote node pub key")
		return err
	}

	var parsedPreSharedkey wgtypes.Key
	if preSharedKey != "" {
		parsedPreSharedkey, err = wgtypes.ParseKey(preSharedKey)
		if err != nil {
			i.runelog.Logger.Errorf("failed to wg preshared key")
			return err
		}
	}

	peer := wgtypes.PeerConfig{
		PublicKey:                   parsedRemoteNodePubKey,
		ReplaceAllowedIPs:           true,
		AllowedIPs:                  []net.IPNet{*ipNet},
		PersistentKeepaliveInterval: &keepAlive,
		PresharedKey:                &parsedPreSharedkey,
		Endpoint:                    endpoint,
	}

	config := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	}

	err = i.configureDevice(config)
	if err != nil {
		i.runelog.Logger.Errorf("failed to configure device")
		return err
	}

	return nil
}

func (i *Iface) configureDevice(config wgtypes.Config) error {
	wg, err := wgctrl.New()
	if err != nil {
		i.runelog.Logger.Errorf("failed to wgctl")
		return err
	}
	defer wg.Close()

	_, err = wg.Device(i.Tun)
	if err != nil {
		i.runelog.Logger.Errorf("failed to wgdevice [%s], %s", i.Tun, err.Error())
		return err
	}

	return wg.ConfigureDevice(i.Tun, config)
}

func (i *Iface) RemoveRemotePeer(iface string, remoteip, remotePeerPubKey string) error {
	i.runelog.Logger.Debugf("delete %s on %s", remotePeerPubKey, i.Tun)

	peerKeyParsed, err := wgtypes.ParseKey(remotePeerPubKey)
	if err != nil {
		return err
	}

	peer := wgtypes.PeerConfig{
		Remove:    true,
		PublicKey: peerKeyParsed,
	}

	config := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	}

	return i.configureDevice(config)
}

func (i *Iface) CreateWithUserSpace(address string) error {
	// proxy
	// listenAddr := "0.0.0.0:2000"

	ip, _, err := net.ParseCIDR(address)
	tunIface, _, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr(ip.String())},
		[]netip.Addr{},
		wg.DefaultMTU,
	)
	if err != nil {
		return err
	}

	tunDevice := device.NewDevice(tunIface, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, "wissy: "))
	err = tunDevice.Up()
	if err != nil {
		return err
	}

	uapi, err := getUAPI(i.Tun)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := uapi.Accept()
			if err != nil {
				fmt.Printf("uapi accept failed with error: %v\n", err)
				continue
			}
			go tunDevice.IpcHandle(conn)
		}
	}()

	err = assignAddr(i.Tun, address)
	if err != nil {
		return err
	}

	return nil
}
