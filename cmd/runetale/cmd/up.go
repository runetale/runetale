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
	"github.com/runetale/runetale/net/rntun"
	"github.com/runetale/runetale/net/runedial"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/process"
	"github.com/runetale/runetale/rnengine"
	"github.com/runetale/runetale/rnengine/router"
	"github.com/runetale/runetale/rsystemd"
	"github.com/runetale/runetale/types/flagtype"
	"github.com/runetale/runetale/utils/ports"
	"github.com/runetale/runetale/wg"
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

	wgPubkey, err := c.ClientConfig.GetWgPublicKey()
	if err != nil {
		fmt.Printf("cannot get wg pubkey, because %s\n", err.Error())
		return err
	}

	ip, cidr, err := loginNode(upArgs.composeKey, c.NodePubKey, wgPubkey, c.ServerClient)
	if err != nil {
		fmt.Printf("failed to login %s\n", err.Error())
		return err
	}
	fmt.Println(ip)
	fmt.Println(cidr)

	port, err := ports.GetFreePort(0)
	if err != nil {
		return err
	}
	if port != wg.DefaultWgPort {
		fmt.Printf("using %d as wireguard port: %d is in use", port, wg.DefaultWgPort)
		return err
	}

	sys := new(rsystemd.System)
	err = createEngine(
		sys, c.ClientConfig.TunName, c.ClientConfig.BlackList, logger, c.SignalClient, c.ServerClient,
		c.NodePubKey, port, c.ClientConfig)
	if err != nil {
		fmt.Printf("failed to create Engine %s\n", err.Error())
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

func loginNode(composeKey, nk, wgPubKey string, client grpc_client.ServerClientImpl) (string, string, error) {
	if composeKey != "" {
		res, err := client.ComposeNode(composeKey, nk, wgPubKey)
		if err != nil {
			return "", "", err
		}
		return res.GetIp(), res.GetCidr(), nil
	}

	res, err := client.LoginNode(nk, wgPubKey)
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

func newLocalBackend() {
	// create userspaceEngine
	// create netstack
	// create local api server
}

func createEngine(sys *rsystemd.System, tunName string, blackList []string, log *log.Logger,
	signalClient grpc_client.SignalClientImpl,
	serverClient grpc_client.ServerClientImpl,
	nodeKey string,
	wgPort uint16,
	clientConfig *conf.ClientConfig,
) (err error) {
	err = tryEngine(log, false, tunName, blackList, sys, signalClient, serverClient, nodeKey, wgPort, clientConfig)
	return
}

func tryEngine(log *log.Logger, isNetStack bool, tunName string,
	blackList []string, sys *rsystemd.System,
	signalClient grpc_client.SignalClientImpl,
	serverClient grpc_client.ServerClientImpl,
	nodeKey string,
	wgPort uint16,
	clientConfig *conf.ClientConfig,
) error {
	// set dialier for netstack
	// todo (snt) dnsのhttps outbound通信を行うために
	// dialerをserveするserverを作る
	dialer := &runedial.Dialer{Logger: log}
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

	// set IfaceBlacklist
	conf.IfaceBlackList = blackList

	// set signal and server
	conf.ServerClient = serverClient
	conf.SignalClient = signalClient

	// set nodekey
	conf.NodeKey = nodeKey

	// set client config
	conf.ClientConfig = clientConfig

	// set client config
	conf.WgListenPort = wgPort

	engine, err := rnengine.NewUserspaceEngine(log, conf)
	if err != nil {
		return err
	}
	sys.Set(engine)

	return nil
}
