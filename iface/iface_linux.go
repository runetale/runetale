// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package iface

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/runetale/runetale/distro"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/utils"
	"github.com/runetale/runetale/wireguard"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func isWireGuardModule(
	runelog *runelog.runelog,
) bool {
	_, err := utils.ExecCmd("modinfo wireguard")
	return err == nil
}

func CreateIface(
	i *Iface,
	runelog *runelog.runelog,
) error {
	addr := i.IP + "/" + i.CIDR

	if distro.Get() == distro.NixOS {
		return createWithKernelSpace(i.Tun, i.WgPrivateKey, addr, runelog)
	}

	if isWireGuardModule(runelog) {
		return createWithKernelSpace(i.Tun, i.WgPrivateKey, addr, runelog)
	}
	return createWithUserSpace(i, addr)
}

func RemoveIface(
	tunname string,
	runelog *runelog.runelog,
) error {
	ipCmd, err := exec.LookPath("ip")
	if err != nil {
		runelog.Logger.Errorf("failed to ip command, because %s", err.Error())
		return err
	}

	_, err = utils.ExecCmd(ipCmd + " link delete dev " + tunname)
	if err != nil {
		runelog.Logger.Errorf("failed to link del, because %s", err.Error())
	}
	return nil
}

func createWithKernelSpace(
	ifaceName, privateKey, address string,
	runelog *runelog.runelog,
) error {
	ipCmd, err := exec.LookPath("ip")
	if err != nil {
		runelog.Logger.Errorf("failed to ip command: %s", err.Error())
		return err
	}

	key, err := wgtypes.ParseKey(privateKey)
	if err != nil {
		runelog.Logger.Errorf("failed to parsing private key: %s", err.Error())
		return err
	}

	wgClient, err := wgctrl.New()
	if err != nil {
		runelog.Logger.Errorf("failed to wireguard client: %s", err.Error())
		return err
	}
	defer wgClient.Close()

	_, err = utils.ExecCmd(ipCmd + " link add dev " + ifaceName + " type wireguard ")
	if err != nil {
		runelog.Logger.Errorf("failed to link add dev. ifaceName: [%s]", ifaceName)
		return err
	}

	_, err = utils.ExecCmd(ipCmd + " address add dev " + ifaceName + " " + address)
	if err != nil {
		runelog.Logger.Errorf("failed to address add dev. ifaceName: [%s], address: [%s]", ifaceName, address)
		return err
	}

	fMark := 0
	port := wireguard.WgPort
	wgConf := wgtypes.Config{
		PrivateKey:   &key,
		ReplacePeers: false,
		FirewallMark: &fMark,
		ListenPort:   &port,
	}

	_, err = wgClient.Device(ifaceName)
	if err != nil {
		runelog.Logger.Errorf("failed to create wireguard device. ifaceName: [%s]", ifaceName)
		return err
	}

	err = wgClient.ConfigureDevice(ifaceName, wgConf)
	if err != nil {
		if os.IsNotExist(err) {
			runelog.Logger.Errorf("device does not exist %s.", ifaceName)
		} else {
			runelog.Logger.Errorf("%s.", err.Error())
		}
		return err
	}

	if _, err := utils.ExecCmd(ipCmd + " link set up dev " + ifaceName); err != nil {
		runelog.Logger.Errorf("%s, %s", ifaceName, err.Error())
		return err
	}
	return nil
}

func assignAddr(tunname, address string) error {
	ip := strings.Split(address, "/")
	cmd := exec.Command("ifconfig", tunname, "inet", address, ip[0])
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Command: %v failed with output %s and error: %v", cmd.String(), out, err)
		return err
	}

	_, resolvedNet, err := net.ParseCIDR(address)
	if err != nil {
		return err
	}

	err = addRoute(tunname, resolvedNet)
	if err != nil {
		fmt.Printf("Adding route failed with error: %v", err)
	}
	return nil
}

func createWithUserSpace(i *Iface, address string) error {
	err := i.CreateWithUserSpace(address)
	if err != nil {
		return err
	}

	key, err := wgtypes.ParseKey(i.WgPrivateKey)
	if err != nil {
		return err
	}

	fwmark := 0
	port := wireguard.WgPort
	config := wgtypes.Config{
		PrivateKey:   &key,
		ReplacePeers: false,
		FirewallMark: &fwmark,
		ListenPort:   &port,
	}
	return i.configureDevice(config)
}
