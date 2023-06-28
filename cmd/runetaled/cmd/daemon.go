// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"fmt"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/daemon"
	dd "github.com/runetale/runetale/daemon/runetaled"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/runelog"
)

var daemonArgs struct {
	logFile  string
	logLevel string
	debug    bool
}

var daemonCmd = &ffcli.Command{
	Name:       "daemon",
	ShortUsage: "daemon <subcommand> [command flags]",
	ShortHelp:  "Install and uninstall daemons, etc",
	Exec:       func(context.Context, []string) error { return flag.ErrHelp },
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("up", flag.ExitOnError)
		fs.StringVar(&daemonArgs.logFile, "logfile", paths.DefaultClientLogFile(), "set logfile path")
		fs.StringVar(&daemonArgs.logLevel, "loglevel", runelog.InfoLevelStr, "set log level")
		fs.BoolVar(&daemonArgs.debug, "debug", false, "is debug")
		return fs
	})(),
	Subcommands: []*ffcli.Command{
		installDaemonCmd,
		uninstallDaemonCmd,
	},
}

var installDaemonCmd = &ffcli.Command{
	Name:       "install",
	ShortUsage: "install",
	ShortHelp:  "install the daemon",
	Exec:       installDaemon,
}

func installDaemon(ctx context.Context, args []string) error {
	runelog, err := runelog.Newrunelog("runetaled daemon", daemonArgs.logLevel, daemonArgs.logFile, daemonArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger: %v", err)
		return nil
	}

	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)
	err = d.Install()
	if err != nil {
		return err
	}
	fmt.Println("success in install")
	return nil
}

var uninstallDaemonCmd = &ffcli.Command{
	Name:       "uninstall",
	ShortUsage: "uninstall",
	ShortHelp:  "uninstall the daemon",
	Exec:       uninstallDaemon,
}

func uninstallDaemon(ctx context.Context, args []string) error {
	runelog, err := runelog.Newrunelog("runetaled daemon", daemonArgs.logLevel, daemonArgs.logFile, daemonArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger: %v", err)
		return nil
	}

	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)
	err = d.Uninstall()
	if err != nil {
		return err
	}

	fmt.Println("uninstall succesfull")
	return nil
}
