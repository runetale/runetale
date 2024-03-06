// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"fmt"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/runelog"
)

// TODO: (shinta) close unix domain socket, system resouce handler, etc.

var downArgs struct {
	logFile  string
	logLevel string
	debug    bool
}

var downCmd = &ffcli.Command{
	Name:       "up",
	ShortUsage: "up [flags]",
	ShortHelp:  "up to runetale, communication client of runetale",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("down", flag.ExitOnError)
		fs.StringVar(&downArgs.logFile, "logfile", paths.DefaultClientLogFile(), "set logfile path")
		fs.StringVar(&downArgs.logLevel, "loglevel", runelog.InfoLevelStr, "set log level")
		fs.BoolVar(&downArgs.debug, "debug", false, "is debug")
		return fs
	})(),
	Exec: execDown,
}

func execDown(ctx context.Context, args []string) error {
	_, err := runelog.NewRunelog("runetale up", downArgs.logLevel, downArgs.logFile, downArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger. because %v", err)
		return nil
	}
	return nil
}
