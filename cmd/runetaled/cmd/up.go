// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/peterbourgon/ff/v2/ffcli"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/daemon"
	dd "github.com/runetale/runetale/daemon/runetaled"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/rcn"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/types/flagtype"
)

var upArgs struct {
	clientPath string
	signalHost string
	signalPort int64
	serverHost string
	serverPort int64
	logFile    string
	logLevel   string
	debug      bool
	daemon     bool
}

var upCmd = &ffcli.Command{
	Name:       "up",
	ShortUsage: "up [flags]",
	ShortHelp:  "command to start runetaled",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("up", flag.ExitOnError)
		fs.StringVar(&upArgs.clientPath, "path", paths.DefaultClientConfigFile(), "client default config file")
		fs.StringVar(&upArgs.serverHost, "server-host", "https://api.caterpie.runetale.com", "grpc server host url")
		fs.Int64Var(&upArgs.serverPort, "server-port", flagtype.DefaultServerPort, "grpc server host port")
		fs.StringVar(&upArgs.signalHost, "signal-host", "https://signal.caterpie.runetale.com", "signaling server host url")
		fs.Int64Var(&upArgs.signalPort, "signal-port", flagtype.DefaultSignalingServerPort, "signaling server host port")
		fs.StringVar(&upArgs.logFile, "logfile", paths.DefaultRunetaledLogFile(), "set logfile path")
		fs.StringVar(&upArgs.logLevel, "loglevel", runelog.InfoLevelStr, "set log level")
		fs.BoolVar(&upArgs.debug, "debug", false, "for debug")
		fs.BoolVar(&upArgs.daemon, "daemon", true, "whether to install daemon")
		return fs
	})(),
	Exec: execUp,
}

// t_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMDgzNDkzNTk5NDg0ODU5MTQzMzUiLCJkZXBsb3ltZW50X21ldGhvZCI6IkRPQ0tFUiIsIm5hbWUiOiJ0ZXN0IiwiZGVzYyI6InRlc3QiLCJwb3J0Ijo4MCwiZXhwIjoxNzE5NTQzMjE0LCJqdGkiOiJiNjlkMjk1Yy0wZmQ4LTRjMmQtYmZiNC02OWQ5NWZjYjc1YmQiLCJpYXQiOjE3MTY5NTEyMTQsImlzcyI6InJ1bmV0YWxlIn0.gtzJdihNEEgraBCxE0mYUFfUVx1q2erS9kKR_nKpSBY
func execUp(ctx context.Context, args []string) error {
	runelog, err := runelog.NewRunelog("runetaled up", upArgs.logLevel, upArgs.logFile, upArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger. because %v", err)
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
		return err
	}

	res, err := conf.ServerClient.LoginMachine(conf.MachinePubKey, conf.Spec.WgPrivateKey)
	if err != nil {
		runelog.Logger.Warnf("failed to login, %s", err.Error())
		return nil
	}

	ch := make(chan struct{})

	r := rcn.NewRcn(conf, conf.MachinePubKey, ch, runelog)

	if upArgs.daemon {
		d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)
		err = d.Install()
		if err != nil {
			runelog.Logger.Errorf("failed to install runetaled. %v", err)
			return err
		}

		runelog.Logger.Infof("launched runetaled daemon.\n")

		return nil
	}

	err = r.Setup(res.Ip, res.Cidr)
	if err != nil {
		runelog.Logger.Debugf("failed to rcn setup, %s", err.Error())
		return err
	}

	go r.Start()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c,
			os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGINT,
		)
		select {
		case <-c:
			close(ch)
		case <-ctx.Done():
			close(ch)
		}
	}()
	<-ch

	r.Stop()

	return nil
}
