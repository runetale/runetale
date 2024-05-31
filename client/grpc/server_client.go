// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"context"

	"github.com/runetale/client-go/runetale/runetale/v1/daemon"
	"github.com/runetale/client-go/runetale/runetale/v1/login"
	"github.com/runetale/client-go/runetale/runetale/v1/machine"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/system"
	"github.com/runetale/runetale/utils"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerClientImpl interface {
	LoginMachine(mk, wgPrivKey string) (*login.LoginMachineResponse, error)
	ComposeMachine(token, mk, wgPrivKey string) (*machine.ComposeMachineResponse, error)
	SyncRemoteMachinesConfig(mk, wgPrivKey string) (*machine.SyncMachinesResponse, error)
	ConnectStreamPeerLoginSession(mk string) (*login.PeerLoginSessionResponse, error)
	Connect(mk string) (*daemon.GetConnectionStatusResponse, error)
	Disconnect(mk string) (*daemon.GetConnectionStatusResponse, error)
	GetConnectionStatus(mk string) (*daemon.GetConnectionStatusResponse, error)
}

type ServerClient struct {
	sysInfo       system.SysInfo
	machineClient machine.MachineServiceClient
	daemonClient  daemon.DaemonServiceClient
	loginClient   login.LoginServiceClient
	conn          *grpc.ClientConn
	ctx           context.Context
	runelog       *runelog.Runelog
}

func NewServerClient(
	sysInfo system.SysInfo,
	conn *grpc.ClientConn,
	runelog *runelog.Runelog,
) ServerClientImpl {
	return &ServerClient{
		sysInfo:       sysInfo,
		machineClient: machine.NewMachineServiceClient(conn),
		daemonClient:  daemon.NewDaemonServiceClient(conn),
		loginClient:   login.NewLoginServiceClient(conn),
		conn:          conn,
		ctx:           context.Background(),
		runelog:       runelog,
	}
}

func (c *ServerClient) LoginMachine(mk, wgPrivKey string) (*login.LoginMachineResponse, error) {
	var (
		ip   string
		cidr string
	)

	parsedKey, err := wgtypes.ParseKey(wgPrivKey)
	if err != nil {
		return nil, err
	}

	md := metadata.New(map[string]string{utils.MachineKey: mk, utils.WgPubKey: parsedKey.PublicKey().String()})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	res, err := c.loginClient.LoginMachine(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	if !res.IsRegistered {
		ip, cidr, err = c.loginBySession(mk, res.LoginUrl)
		if err != nil {
			return nil, err
		}
	} else {
		ip = res.Ip
		cidr = res.Cidr
	}

	c.runelog.Logger.Infof("runetale ip => [%s/%s]", ip, cidr)

	return res, nil
}

func (c *ServerClient) loginBySession(mk, url string) (string, string, error) {
	err := utils.OpenBrowser(url)
	if err != nil {
		return "", "", err
	}

	msg, err := c.ConnectStreamPeerLoginSession(mk)
	if err != nil {
		return "", "", err
	}

	return msg.Ip, msg.Cidr, nil
}

func (c *ServerClient) ComposeMachine(token, mk, wgPrivKey string) (*machine.ComposeMachineResponse, error) {
	parsedKey, err := wgtypes.ParseKey(wgPrivKey)
	if err != nil {
		return nil, err
	}

	md := metadata.New(map[string]string{utils.AccessToken: token, utils.MachineKey: mk, utils.WgPubKey: parsedKey.PublicKey().String(), utils.HostName: c.sysInfo.Hostname, utils.OS: c.sysInfo.OS})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	res, err := c.machineClient.ComposeMachine(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *ServerClient) ConnectStreamPeerLoginSession(mk string) (*login.PeerLoginSessionResponse, error) {
	var (
		msg = &login.PeerLoginSessionResponse{}
	)

	md := metadata.New(map[string]string{utils.MachineKey: mk, utils.HostName: c.sysInfo.Hostname, utils.OS: c.sysInfo.OS})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	stream, err := c.loginClient.StreamPeerLoginSession(newctx, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	header, err := stream.Header()
	if err != nil {
		return nil, err
	}

	sessionid := getLoginSessionID(header)
	c.runelog.Logger.Debugf("sessionid: [%s]", sessionid)

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

func (c *ServerClient) SyncRemoteMachinesConfig(mk, wgPrivKey string) (*machine.SyncMachinesResponse, error) {
	parsedKey, err := wgtypes.ParseKey(wgPrivKey)
	if err != nil {
		return nil, err
	}

	md := metadata.New(map[string]string{utils.MachineKey: mk, utils.WgPubKey: parsedKey.PublicKey().String()})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	conf, err := c.machineClient.SyncRemoteMachinesConfig(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func (c *ServerClient) Connect(mk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.MachineKey: mk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.Connect(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (c *ServerClient) Disconnect(mk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.MachineKey: mk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.Disconnect(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (c *ServerClient) GetConnectionStatus(mk string) (*daemon.GetConnectionStatusResponse, error) {
	md := metadata.New(map[string]string{utils.MachineKey: mk})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	status, err := c.daemonClient.GetConnectionStatus(newctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return status, nil
}
