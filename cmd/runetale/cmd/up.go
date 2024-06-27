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
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/net/dial"
	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/process"
	"github.com/runetale/runetale/rnengine"
	"github.com/runetale/runetale/rnengine/router"
	runesystemd "github.com/runetale/runetale/rune_systemd"
	"github.com/runetale/runetale/types/flagtype"
)

var upArgs struct {
	clientPath string
	composeKey string
	serverHost string
	serverPort int64
	signalHost string
	signalPort int64
	logFile    string
	logLevel   string
	debug      bool
}

var upCmd = &ffcli.Command{
	Name:       "up",
	ShortUsage: "up [flags]",
	ShortHelp:  "up to runetale, communication client of runetale",
	FlagSet: (func() *flag.FlagSet {
		fs := flag.NewFlagSet("up", flag.ExitOnError)
		fs.StringVar(&upArgs.clientPath, "path", paths.DefaultClientConfigFile(), "client default config file")
		fs.StringVar(&upArgs.serverHost, "server-host", "https://api.caterpie.runetale.com", "server host")
		fs.StringVar(&upArgs.composeKey, "compose-key", "", "launch node with access token")
		fs.Int64Var(&upArgs.serverPort, "server-port", flagtype.DefaultServerPort, "grpc server host port")
		fs.StringVar(&upArgs.signalHost, "signal-host", "https://signal.caterpie.runetale.com", "signal server host")
		fs.Int64Var(&upArgs.signalPort, "signal-port", flagtype.DefaultSignalingServerPort, "signal server port")
		fs.StringVar(&upArgs.logFile, "logfile", paths.DefaultClientLogFile(), "set logfile path")
		fs.StringVar(&upArgs.logLevel, "loglevel", log.DebugLevelStr, "set log level")
		fs.BoolVar(&upArgs.debug, "debug", false, "is debug")
		return fs
	})(),
	Exec: execUp,
}

// after login, check to see if the runetaled daemon is up.
// if not, prompt the user to start it.
func execUp(ctx context.Context, args []string) error {
	logger, err := log.NewLogger("runetale up", upArgs.logLevel, upArgs.logFile, upArgs.debug)
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
		logger,
	)
	if err != nil {
		fmt.Printf("failed to create client conf, because %s\n", err.Error())
		return err
	}

	// if !isInstallRunetaledDaemon(logger) && !isRunningRunetaledProcess(logger) {
	// 	logger.Logger.Warnf("you need to activate runetaled. execute this command 'runetaled up'")
	// 	return nil
	// }

	ip, cidr, err := loginNode(upArgs.composeKey, c.NodePubKey, c.Spec.WgPrivateKey, c.ServerClient)
	if err != nil {
		fmt.Printf("failed to login %s\n", err.Error())
		return err
	}
	fmt.Println(ip)
	fmt.Println(cidr)

	// err = upEngine(
	// 	ctx,
	// 	c.ServerClient,
	// 	logger,
	// 	c.Spec.TunName,
	// 	c.NodePubKey,
	// 	ip,
	// 	cidr,
	// 	c.Spec.WgPrivateKey,
	// 	c.Spec.BlackList,
	// )
	// if err != nil {
	// 	logger.Logger.Warnf("failed to start engine. because %v", err)
	// 	return err
	// }

	sys := new(runesystemd.System)
	tryEngine(logger, false, c.Spec.TunName, sys)

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

func loginNode(composeKey, nk, wgPrivKey string, client grpc_client.ServerClientImpl) (string, string, error) {
	if composeKey != "" {
		res, err := client.ComposeNode(composeKey, nk, wgPrivKey)
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

func isInstallRunetaledDaemon(logger *log.Logger) bool {
	d := daemon.NewDaemon(dd.BinPath, dd.ServiceName, dd.DaemonFilePath, dd.SystemConfig, logger)
	_, isInstalled := d.Status()
	return isInstalled
}

func isRunningRunetaledProcess(logger *log.Logger) bool {
	p := process.NewProcess(logger)
	return p.GetRunetaledProcess()
}

func getLocalBackend() {
	// create userspaceEngine
	// creeate netstack
	//
}

func createEngine(sys *runesystemd.System, tunName string, log *log.Logger) (err error) {
	err = tryEngine(log, false, tunName, sys)
	return
}

// TODO(snt)
// netstack onlyのモードの対応、tunを作らずに実装する
// netstack-onlyの場合はこの関数の中でtunの影武者が作られる
// 影武者を使って通信
// config.Tun = rntun.NewKagemusha()
func tryEngine(log *log.Logger, isNetStack bool, tunName string, sys *runesystemd.System) error {
	dialer := &dial.Dialer{Logger: log}
	conf := rnengine.Config{
		Dialer: dialer,
	}
	sys.Set(dialer)

	// set tun
	dev, devName, err := rntun.New(log, tunName)
	if err != nil {
		rntun.Diagnose(log, tunName, err)
		return fmt.Errorf("rntun.New(%q): %w", tunName, err)
	}
	conf.Tun = dev
	log.Logger.Debugf("created tun device: %s", devName)

	// set router
	r, err := router.New(dev, log)
	if err != nil {
		dev.Close()
		return err
	}
	conf.Router = r
	sys.Set(conf.Router)

	err = rnengine.NewUserspaceEngine(log, conf)
	if err != nil {
		return err
	}

	return nil
}
