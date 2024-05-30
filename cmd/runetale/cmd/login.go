// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/types/flagtype"
)

var loginArgs struct {
	clientPath  string
	accessToken string
	serverHost  string
	serverPort  int64
	signalHost  string
	signalPort  int64
	logFile     string
	logLevel    string
	debug       bool
}

var loginCmd = &ffcli.Command{
	Name:       "login",
	ShortUsage: "login [flags]",
	ShortHelp:  "login to runetale, start the management server and then run it",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("login", flag.ExitOnError)
		fs.StringVar(&loginArgs.accessToken, "access-token", "", "launch peer with access token")
		fs.StringVar(&loginArgs.clientPath, "path", paths.DefaultClientConfigFile(), "client default config file")
		fs.StringVar(&loginArgs.serverHost, "server-host", "https://api.caterpie.runetale.com", "server host")
		fs.Int64Var(&loginArgs.serverPort, "server-port", flagtype.DefaultServerPort, "grpc server host port")
		fs.StringVar(&loginArgs.signalHost, "signal-host", "https://signal.caterpie.runetale.com", "signal server host")
		fs.Int64Var(&loginArgs.signalPort, "signal-port", flagtype.DefaultSignalingServerPort, "signal server port")
		fs.StringVar(&loginArgs.logFile, "logfile", paths.DefaultClientLogFile(), "set logfile path")
		fs.StringVar(&loginArgs.logLevel, "loglevel", runelog.InfoLevelStr, "set log level")
		fs.BoolVar(&loginArgs.debug, "debug", false, "for debug logging")
		return fs
	})(),
	Exec: execLogin,
}

func execLogin(ctx context.Context, args []string) error {
	runelog, err := runelog.NewRunelog("runetale login", loginArgs.logLevel, loginArgs.logFile, loginArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger. because %v\n", err)
		return nil
	}

	clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	c, err := conf.NewConf(
		clientCtx, loginArgs.clientPath,
		loginArgs.debug,
		loginArgs.serverHost, uint(loginArgs.serverPort),
		loginArgs.signalHost, uint(loginArgs.signalPort),
		runelog,
	)
	if err != nil {
		fmt.Printf("failed to create client conf, because %s\n", err.Error())
		return nil
	}

	_, err = c.ServerClient.LoginMachine(c.MachinePubKey, c.Spec.WgPrivateKey)
	if err != nil {
		runelog.Logger.Warnf("failed to login, %s", err.Error())
		return nil
	}

	ip, cidr, err := loginMachine(loginArgs.accessToken, c.MachinePubKey, c.Spec.WgPrivateKey, c.ServerClient)
	if err != nil {
		fmt.Printf("failed to login %s\n", err.Error())
		return err
	}

	runelog.Logger.Infof("runetale ip => [%s/%s]\n", ip, cidr)

	return nil
}
