package conf

import (
	"context"

	grpc_client "github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/paths"
	"github.com/runetale/runetale/rcn/conn"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/store"
	"google.golang.org/grpc"
)

type Conf struct {
	SignalClient  grpc_client.SignalClientImpl
	ServerClient  grpc_client.ServerClientImpl
	Spec          *Spec
	MachinePubKey string
	runelog       *runelog.Runelog
}

func NewConf(
	clientCtx context.Context,
	path string,
	isDev bool,
	serverHost string, serverPort uint,
	signalHost string, signalPort uint,
	runelog *runelog.Runelog,
) (*Conf, error) {
	// configure file store
	//
	cfs, err := store.NewFileStore(paths.DefaultRunetaleClientStateFile(), runelog)
	if err != nil {
		runelog.Logger.Warnf("failed to create clietnt state, because %v", err)
		return nil, err
	}

	// configure client store
	//
	cs := store.NewClientStore(cfs, runelog)
	err = cs.WritePrivateKey()
	if err != nil {
		runelog.Logger.Warnf("failed to write client state private key, because %v", err)
		return nil, err
	}

	// initialize client config
	//
	spec, err := NewSpec(
		path,
		serverHost, uint(serverPort),
		signalHost, uint(signalPort),
		isDev,
		runelog,
	)
	if err != nil {
		runelog.Logger.Warnf("failed to initialize client core, because %v", err)
		return nil, err
	}

	spec = spec.CreateSpec()

	option := grpc_client.NewGrpcDialOption(runelog, isDev)

	runelog.Logger.Infof("connecting to [%s]", spec.GetServerHost())
	serverClient, err := setupGrpcServerClient(clientCtx, spec.GetServerHost(), runelog, option)
	if err != nil {
		runelog.Logger.Warnf("failed to initialize grpc server client. because %v", err)
		return nil, err
	}
	runelog.Logger.Infof("connect succeded [%s]", spec.GetServerHost())

	runelog.Logger.Infof("connecting to [%s]", spec.GetSignalHost())
	signalClient, err := setupGrpcSignalClient(clientCtx, spec.GetSignalHost(), runelog, option)
	if err != nil {
		runelog.Logger.Warnf("failed to initialize grpc signal client. because %v", err)
		return nil, err
	}
	runelog.Logger.Infof("connect succeded [%s]", spec.GetSignalHost())

	return &Conf{
		SignalClient:  signalClient,
		ServerClient:  serverClient,
		Spec:          spec,
		MachinePubKey: cs.GetPublicKey(),
	}, nil

}

func setupGrpcServerClient(
	clientctx context.Context,
	url string,
	runelog *runelog.Runelog,
	option grpc.DialOption,
) (grpc_client.ServerClientImpl, error) {
	sconn, err := grpc.DialContext(
		clientctx,
		url,
		option,
		grpc.WithBlock(),
	)

	serverClient := grpc_client.NewServerClient(sconn, runelog)
	if err != nil {
		runelog.Logger.Warnf("failed to connect server client, because %v", err)
		return nil, err
	}

	return serverClient, err
}

func setupGrpcSignalClient(
	clientctx context.Context,
	url string,
	runelog *runelog.Runelog,
	option grpc.DialOption,
) (grpc_client.SignalClientImpl, error) {
	gconn, err := grpc.DialContext(
		clientctx,
		url,
		option,
		grpc.WithBlock(),
	)
	if err != nil {
		runelog.Logger.Warnf("failed to connect signal client, because %v", err)
		return nil, err
	}

	connState := conn.NewConnectedState()

	signalClient := grpc_client.NewSignalClient(gconn, connState, runelog)

	return signalClient, err
}
