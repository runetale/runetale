// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

// the down cmd terminates the runetaled daemon process and closes
// the p2p connection

package cmd

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/daemon"
	dd "github.com/runetale/runetale/daemon/runetaled"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/rcn"
	"github.com/runetale/runetale/runelog"
)

var downArgs struct {
	logFile  string
	logLevel string
	debug    bool
}

var downCmd = &ffcli.Command{
	Name:      "down",
	ShortHelp: "down the runetaled",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("down", flag.ExitOnError)
		fs.StringVar(&downArgs.logFile, "logfile", paths.DefaultRunetaledLogFile(), "set logfile path")
		fs.StringVar(&downArgs.logLevel, "loglevel", runelog.InfoLevelStr, "set log level")
		fs.BoolVar(&downArgs.debug, "debug", false, "is debug")
		return fs
	})(),
	Exec: execDown,
}

// uninstall runetaled and delete wireguard interface
func execDown(ctx context.Context, args []string) error {
	runelog, err := runelog.NewRunelog("runetaled down", downArgs.logLevel, downArgs.logFile, downArgs.debug)
	if err != nil {
		fmt.Println("failed to initialize logger")
		return nil
	}

	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)

	_, isInstalled := d.Status()
	if !isInstalled {
		fmt.Println("already terminated")
		return nil
	}

	clientCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conf, err := conf.NewConf(
		clientCtx,
		upArgs.clientPath,
		upArgs.debug,
		upArgs.serverHost,
		uint(upArgs.serverPort),
		upArgs.signalHost,
		uint(upArgs.signalPort),
		runelog,
	)
	if err != nil {
		fmt.Printf("failed to create client conf, because %s\n", err.Error())
		return nil
	}

	r := rcn.NewRcn(conf, conf.NodePubKey, nil, runelog)

	err = r.Stop()
	if err != nil {
		fmt.Println("failed to uninstall runetale")
		return nil
	}

	err = d.Uninstall()
	if err != nil {
		fmt.Println("failed to uninstall runetale")
		return nil
	}

	return nil
}
