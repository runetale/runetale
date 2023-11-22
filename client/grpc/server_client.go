// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"fmt"

	"github.com/runetale/client-go/runetale/runetale/v1/daemon"
	"github.com/runetale/client-go/runetale/runetale/v1/login_session"
	"github.com/runetale/client-go/runetale/runetale/v1/machine"
	"github.com/runetale/runetale/system"
	"github.com/runetale/runetale/utils"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerClientImpl interface {
	Login(mk, wgPrivKey string) (*machine.LoginResponse, error)
	SyncRemoteMachinesConfig(mk, wgPrivKey string) (*machine.SyncMachinesResponse, error)
	ConnectStreamPeerLoginSession(mk string) (*login_session.PeerLoginSessionResponse, error)
	Connect(mk string) (*daemon.GetConnectionStatusResponse, error)
	Disconnect(mk string) (*daemon.GetConnectionStatusResponse, error)
	GetConnectionStatus(mk string) (*daemon.GetConnectionStatusResponse, error)
}

type ServerClient struct {
	machineClient      machine.MachineServiceClient
	daemonClient       daemon.DaemonServiceClient
	loginSessionClient login_session.LoginSessionServiceClient
	conn               *grpc.ClientConn
	ctx                context.Context
	runelog            *runelog.runelog
}

func NewServerClient(
	conn *grpc.ClientConn,
	runelog *runelog.runelog,
) ServerClientImpl {
	return &ServerClient{
		machineClient:      machine.NewMachineServiceClient(conn),
		daemonClient:       daemon.NewDaemonServiceClient(conn),
		loginSessionClient: login_session.NewLoginSessionServiceClient(conn),
		conn:               conn,
		ctx:                context.Background(),
		runelog:            runelog,
	}
}

func (c *ServerClient) Login(mk, wgPrivKey string) (*machine.LoginResponse, error) {
	var (
		ip   string
		cidr string
	)

	parsedKey, err := wgtypes.ParseKey(wgPrivKey)
	if err != nil {
		return nil, err
	}

	md := metadata.New(map[string]string{utils.MachineKey: mk, utils.WgPubKey: parsedKey.String()})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	res, err := c.machineClient.Login(ctx, &emptypb.Empty{})
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

	fmt.Printf("runetale ip => [%s/%s]\n", ip, cidr)
	fmt.Printf("Successful login\n")

	return res, nil
}

func (c *ServerClient) loginBySession(mk, url string) (string, string, error) {
	fmt.Printf("please log in via this link => %s\n", url)
	msg, err := c.ConnectStreamPeerLoginSession(mk)
	if err != nil {
		return "", "", err
	}
	return msg.Ip, msg.Cidr, nil
}

func (c *ServerClient) ConnectStreamPeerLoginSession(mk string) (*login_session.PeerLoginSessionResponse, error) {
	var (
		msg = &login_session.PeerLoginSessionResponse{}
	)

	sys := system.NewSysInfo()
	md := metadata.New(map[string]string{utils.MachineKey: mk, utils.HostName: sys.Hostname, utils.OS: sys.OS})
	newctx := metadata.NewOutgoingContext(c.ctx, md)

	stream, err := c.loginSessionClient.StreamPeerLoginSession(newctx, grpc.WaitForReady(true))
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
