package conf

import (
	"context"

	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/rnengine/wonderwall/conn"
	"github.com/runetale/runetale/store"
	"github.com/runetale/runetale/system"
	"google.golang.org/grpc"
)

type Conf struct {
	SignalClient grpc_client.SignalClientImpl
	ServerClient grpc_client.ServerClientImpl
	Spec         *Spec
	NodePubKey   string
}

// note: (snt)
// NewConf creating Conf structure for file store and specs.
// file store cached wg privatekey on node.
// specs cached signal and server hosts
func NewConf(
	clientCtx context.Context,
	path string,
	isDev bool,
	serverHost string, serverPort uint,
	signalHost string, signalPort uint,
	log *log.Logger,
) (*Conf, error) {
	// configure file store
	//
	cfs, err := store.NewFileStore(paths.DefaultRunetaleClientStateFile(), log)
	if err != nil {
		log.Logger.Warnf("failed to create clietnt state, because %v", err)
		return nil, err
	}

	// configure client store
	//
	cs := store.NewClientStore(cfs, log)
	err = cs.WritePrivateKey()
	if err != nil {
		log.Logger.Warnf("failed to write client state private key, because %v", err)
		return nil, err
	}

	// initialize client config
	//
	spec, err := NewSpec(
		path,
		serverHost, uint(serverPort),
		signalHost, uint(signalPort),
		isDev,
		log,
	)
	if err != nil {
		log.Logger.Warnf("failed to initialize client core, because %v", err)
		return nil, err
	}

	spec = spec.CreateSpec()

	option := grpc_client.NewGrpcDialOption(log, isDev)
	sys := system.NewSysInfo()

	log.Logger.Infof("connecting to runetale-server => [%s]", spec.GetServerHost())
	serverClient, err := setupGrpcServerClient(*sys, clientCtx, spec.GetServerHost(), log, option)
	if err != nil {
		log.Logger.Warnf("failed to initialize grpc server client. because %v", err)
		return nil, err
	}
	log.Logger.Infof("connect succeded [%s]", spec.GetServerHost())

	log.Logger.Infof("connecting to runetale-signal-server => [%s]", spec.GetSignalHost())
	signalClient, err := setupGrpcSignalClient(*sys, clientCtx, spec.GetSignalHost(), log, option)
	if err != nil {
		log.Logger.Warnf("failed to initialize grpc signal client. because %v", err)
		return nil, err
	}
	log.Logger.Infof("connect succeded [%s]", spec.GetSignalHost())

	return &Conf{
		SignalClient: signalClient,
		ServerClient: serverClient,
		Spec:         spec,
		NodePubKey:   cs.GetPublicKey(),
	}, nil

}

func setupGrpcServerClient(
	sysInfo system.SysInfo,
	clientctx context.Context,
	url string,
	log *log.Logger,
	option grpc.DialOption,
) (grpc_client.ServerClientImpl, error) {
	sconn, err := grpc.DialContext(
		clientctx,
		url,
		option,
		grpc.WithBlock(),
	)

	serverClient := grpc_client.NewServerClient(sysInfo, sconn, log)
	if err != nil {
		log.Logger.Warnf("failed to connect server client, because %v", err)
		return nil, err
	}

	return serverClient, err
}

func setupGrpcSignalClient(
	sysInfo system.SysInfo,
	clientctx context.Context,
	url string,
	log *log.Logger,
	option grpc.DialOption,
) (grpc_client.SignalClientImpl, error) {
	gconn, err := grpc.DialContext(
		clientctx,
		url,
		option,
		grpc.WithBlock(),
	)
	if err != nil {
		log.Logger.Warnf("failed to connect signal client, because %v", err)
		return nil, err
	}

	connState := conn.NewConnectedState()

	signalClient := grpc_client.NewSignalClient(sysInfo, gconn, connState, log)

	return signalClient, err
}
