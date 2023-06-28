// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

//go:build linux || darwin
// +build linux darwin

package iface

import (
	"fmt"
	"net"
	"os/exec"

	"golang.zx2c4.com/wireguard/ipc"
)

// getUAPI returns a Listener
func getUAPI(iface string) (net.Listener, error) {
	tunSock, err := ipc.UAPIOpen(iface)
	if err != nil {
		return nil, err
	}
	return ipc.UAPIListen(iface, tunSock)
}

func addRoute(tun string, ipNet *net.IPNet) error {
	cmd := exec.Command("route", "add", "-net", ipNet.String(), "-interface", tun)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Command: %v failed with output %s and error: %v", cmd.String(), out, err)
		return err
	}

	return nil
}
