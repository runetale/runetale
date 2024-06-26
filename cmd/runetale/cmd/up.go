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
	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/daemon"
	dd "github.com/runetale/runetale/daemon/runetaled"
	"github.com/runetale/runetale/engine"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/process"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/types/flagtype"
)

var upArgs struct {
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

var upCmd = &ffcli.Command{
	Name:       "up",
	ShortUsage: "up [flags]",
	ShortHelp:  "up to runetale, communication client of runetale",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("up", flag.ExitOnError)
		fs.StringVar(&upArgs.clientPath, "path", paths.DefaultClientConfigFile(), "client default config file")
		fs.StringVar(&upArgs.serverHost, "server-host", "https://api.caterpie.runetale.com", "server host")
		fs.StringVar(&upArgs.accessToken, "access-token", "", "launch node with access token")
		fs.Int64Var(&upArgs.serverPort, "server-port", flagtype.DefaultServerPort, "grpc server host port")
		fs.StringVar(&upArgs.signalHost, "signal-host", "https://signal.caterpie.runetale.com", "signal server host")
		fs.Int64Var(&upArgs.signalPort, "signal-port", flagtype.DefaultSignalingServerPort, "signal server port")
		fs.StringVar(&upArgs.logFile, "logfile", paths.DefaultClientLogFile(), "set logfile path")
		fs.StringVar(&upArgs.logLevel, "loglevel", runelog.DebugLevelStr, "set log level")
		fs.BoolVar(&upArgs.debug, "debug", false, "is debug")
		return fs
	})(),
	Exec: execUp,
}

// after login, check to see if the runetaled daemon is up.
// if not, prompt the user to start it.
func execUp(ctx context.Context, args []string) error {
	runelog, err := runelog.NewRunelog("runetale up", upArgs.logLevel, upArgs.logFile, upArgs.debug)
	if err != nil {
		fmt.Printf("failed to initialize logger. because %v", err)
		return nil
	}

	clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	c, err := conf.NewConf(
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

	if !isInstallRunetaledDaemon(runelog) && !isRunningRunetaledProcess(runelog) {
		runelog.Logger.Warnf("you need to activate runetaled. execute this command 'runetaled up'")
		return nil
	}

	ip, cidr, err := loginNode(upArgs.accessToken, c.NodePubKey, c.Spec.WgPrivateKey, c.ServerClient)
	if err != nil {
		fmt.Printf("failed to login %s\n", err.Error())
		return err
	}

	err = upEngine(
		ctx,
		c.ServerClient,
		runelog,
		c.Spec.TunName,
		c.NodePubKey,
		ip,
		cidr,
		c.Spec.WgPrivateKey,
		c.Spec.BlackList,
	)
	if err != nil {
		runelog.Logger.Warnf("failed to start engine. because %v", err)
		return err
	}

	stop := make(chan struct{})
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c,
			os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGINT,
		)
		select {
		case <-c:
			close(stop)
		case <-ctx.Done():
			close(stop)
		}
	}()
	<-stop

	return nil
}

func loginNode(accessToken, nk, wgPrivKey string, client grpc_client.ServerClientImpl) (string, string, error) {
	if accessToken != "" {
		res, err := client.ComposeNode(accessToken, nk, wgPrivKey)
		if err != nil {
			return "", "", err
		}
		return res.GetIp(), res.GetCidr(), nil
	}

	res, err := client.LoginNode(nk, wgPrivKey)
	if err != nil {
		return "", "", err
	}
	return res.GetIp(), res.GetCidr(), nil
}

func upEngine(
	ctx context.Context,
	serverClient grpc_client.ServerClientImpl,
	runelog *runelog.Runelog,
	tunName string,
	mPubKey string,
	ip string,
	cidr string,
	wgPrivKey string,
	blackList []string,
) error {
	pctx, cancel := context.WithCancel(ctx)

	engine, err := engine.NewEngine(
		serverClient,
		runelog,
		tunName,
		mPubKey,
		ip,
		cidr,
		wgPrivKey,
		blackList,
		pctx,
		cancel,
	)
	if err != nil {
		runelog.Logger.Warnf("failed to connect signal client. because %v", err)
		return err
	}

	err = engine.Start()
	if err != nil {
		runelog.Logger.Warnf("failed to start engine. because %v", err)
		return err
	}

	return nil
}

func isInstallRunetaledDaemon(runelog *runelog.Runelog) bool {
	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, runelog)
	_, isInstalled := d.Status()
	return isInstalled
}

func isRunningRunetaledProcess(runelog *runelog.Runelog) bool {
	p := process.NewProcess(runelog)
	return p.GetRunetaledProcess()
}
