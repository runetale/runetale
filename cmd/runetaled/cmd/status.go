// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/daemon"
	dd "github.com/runetale/runetale/daemon/runetaled"
	"github.com/runetale/runetale/runelog"
)

var statusArgs struct {
	logFile  string
	logLevel string
	debug    bool
}

var statusCmd = &ffcli.Command{
	Name:      "status",
	ShortHelp: "status the daemon",
	Subcommands: []*ffcli.Command{
		statusDaemonCmd,
	},
}

var statusDaemonCmd = &ffcli.Command{
	Name:      "daemon",
	ShortHelp: "status the runetaled daemon",
	Exec:      statusDaemon,
}

func statusDaemon(ctx context.Context, args []string) error {
	runelog, err := runelog.NewRunelog("runetaled status", statusArgs.logLevel, statusArgs.logFile, statusArgs.debug)
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
		return err
	}

	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)
	status, _ := d.Status()
	fmt.Println(status)
	return nil
}
