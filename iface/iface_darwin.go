// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package iface

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/utils"
	"github.com/runetale/runetale/wg"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func CreateIface(
	i *Iface,
	runelog *runelog.Runelog,
) error {
	addr := i.IP + "/" + i.CIDR

	err := i.createWithUserSpace(i.Tun, addr)
	if err != nil {
		runelog.Logger.Errorf("failed to create user space, %v", err)
		return err
	}

	key, err := wgtypes.ParseKey(i.WgPrivateKey)
	if err != nil {
		runelog.Logger.Warnf("failed to parsing wireguard private key, %v", err)
		return err
	}

	fwmark := 0
	port := wg.WgPort

	config := wgtypes.Config{
		PrivateKey:   &key,
		ReplacePeers: false,
		FirewallMark: &fwmark,
		ListenPort:   &port,
	}

	return i.configureDevice(config)
}

func RemoveIface(
	tunname string,
	runelog *runelog.Runelog,
) error {
	ipCmd, err := exec.LookPath("ifconfig")
	if err != nil {
		runelog.Logger.Errorf("failed to lookup ip command, %s", err.Error())
		return err
	}

	_, err = utils.ExecCmd(ipCmd + fmt.Sprintf(" %s", tunname) + " down")
	if err != nil {
		runelog.Logger.Errorf("failed to ifconfig delete, because %s", err.Error())
	}

	return nil
}

func (i *Iface) createWithUserSpace(tunname, address string) error {
	tunIface, err := tun.CreateTUN(tunname, wg.DefaultMTU)
	if err != nil {
		return err
	}

	tunDevice := device.NewDevice(tunIface, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, "runetale: "))
	err = tunDevice.Up()
	if err != nil {
		return err
	}

	uapi, err := getUAPI(tunname)
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

	err = assignAddr(tunname, address)
	if err != nil {
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
