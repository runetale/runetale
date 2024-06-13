// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"context"

	"github.com/runetale/client-go/runetale/runetale/v1/daemon"
	"github.com/runetale/client-go/runetale/runetale/v1/login"
	"github.com/runetale/client-go/runetale/runetale/v1/node"
	"github.com/runetale/runetale/log"
	"github.com/runetale/runetale/system"
	"github.com/runetale/runetale/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerClientImpl interface {
	LoginNode(nk, wgPubKey string) (*login.LoginNodeResponse, error)
	ComposeNode(composeKey, nk, wgPubKey string) (*node.ComposeNodeResponse, error)
	SyncRemoteNodesConfig(nk, wgPubKey string) (*node.SyncNodesResponse, error)
	ConnectLoginSession(nk string) (*login.LoginSessionResponse, error)
	Connect(nk string) (*daemon.GetConnectionStatusResponse, error)
	Disconnect(nk string) (*daemon.GetConnectionStatusResponse, error)
	GetConnectionStatus(nk string) (*daemon.GetConnectionStatusResponse, error)
	GetNetworkMap(nk, wgPubKey string) (*node.NetworkMapResponse, error)
}

type ServerClient struct {
	sysInfo      system.SysInfo
	nodeClient   node.NodeServiceClient
	daemonClient daemon.DaemonServiceClient
	loginClient  login.LoginServiceClient
	conn         *grpc.ClientConn
	ctx          context.Context
	log          *log.Logger
}

func NewServerClient(
	sysInfo system.SysInfo,
	conn *grpc.ClientConn,
	logger *log.Logger,
) ServerClientImpl {
	return &ServerClient{
		sysInfo:      sysInfo,
		nodeClient:   node.NewNodeServiceClient(conn),
		daemonClient: daemon.NewDaemonServiceClient(conn),
		loginClient:  login.NewLoginServiceClient(conn),
		conn:         conn,
		ctx:          context.Background(),
		log:          logger,
	}
}

func (c *ServerClient) LoginNode(nk, wgPubKey string) (*login.LoginNodeResponse, error) {
	var (
		ip   string
		cidr string
	)

	md := metadata.New(map[string]string{utils.NodeKey: nk, utils.WgPubKey: wgPubKey})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	res, err := c.loginClient.LoginNode(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	if !res.IsRegistered {
		ip, cidr, err = c.loginBySession(nk, res.LoginUrl)
		if err != nil {
			return nil, err
		}
	} else {
		ip = res.Ip
		cidr = res.Cidr
	}

	c.log.Logger.Infof("runetale ip => [%s/%s]", ip, cidr)

	return res, nil
}

func (c *ServerClient) loginBySession(nk, url string) (string, string, error) {
	err := utils.OpenBrowser(url)
	if err != nil {
		return "", "", err
	}

	msg, err := c.ConnectLoginSession(nk)
	if err != nil {
		return "", "", err
	}

	return msg.Ip, msg.Cidr, nil
}

func (c *ServerClient) ComposeNode(composeKey, nk, wgPubKey string) (*node.ComposeNodeResponse, error) {
	md := metadata.New(map[string]string{utils.ComposeKey: composeKey, utils.NodeKey: nk, utils.WgPubKey: wgPubKey, utils.HostName: c.sysInfo.Hostname, utils.OS: c.sysInfo.OS})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	res, err := c.nodeClient.ComposeNode(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *ServerClient) ConnectLoginSession(nk string) (*login.LoginSessionResponse, error) {
	var (
		msg = &login.LoginSessionResponse{}
	)

	md := metadata.New(map[string]string{utils.NodeKey: nk, utils.HostName: c.sysInfo.Hostname, utils.OS: c.sysInfo.OS})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	stream, err := c.loginClient.LoginSession(newctx, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	header, err := stream.Header()
	if err != nil {
		return nil, err
	}

	sessionid := getLoginSessionID(header)
	c.log.Logger.Debugf("sessionid: [%s]", sessionid)

	for {
		msg, err = stream.Recv()
		if err != nil {
			return nil, err
		}

		err = stream.Send(&emptypb.Empty{})
		if err != nil {
			return nil, err
		}
		break
	}
	return msg, nil
}

func (c *ServerClient) SyncRemoteNodesConfig(nk, wgPubKey string) (*node.SyncNodesResponse, error) {
	md := metadata.New(map[string]string{utils.NodeKey: nk, utils.WgPubKey: wgPubKey})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	conf, err := c.nodeClient.SyncRemoteNodesConfig(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func (c *ServerClient) Connect(nk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.NodeKey: nk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.Connect(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (c *ServerClient) Disconnect(nk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.NodeKey: nk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.Disconnect(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (c *ServerClient) GetConnectionStatus(nk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.NodeKey: nk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.GetConnectionStatus(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (c *ServerClient) GetNetworkMap(nk, wgPubKey string) (*node.NetworkMapResponse, error) {
	md := metadata.New(map[string]string{utils.NodeKey: nk, utils.WgPubKey: wgPubKey})
	ctx := metadata.NewOutgoingContext(c.ctx, md)
	nmap, err := c.nodeClient.GetNetworkMap(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return nmap, nil
}
