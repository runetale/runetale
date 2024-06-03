// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/pion/ice/v2"
	"github.com/runetale/client-go/runetale/runetale/v1/negotiation"
	"github.com/runetale/client-go/runetale/runetale/v1/rtc"
	"github.com/runetale/runetale/rcn/conn"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/system"
	"github.com/runetale/runetale/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type SignalClientImpl interface {
	Candidate(dstnk, srcnk string, candidate ice.Candidate) error
	Offer(dstnk, srcnk string, uFlag string, pwd string) error
	Answer(dstnk, srcnk string, uFlag string, pwd string) error
	Connect(mk string, handler func(msg *negotiation.NegotiationRequest) error) error

	WaitStartConnect()
	IsReady() bool
	GetStunTurnConfig() (*rtc.GetStunTurnConfigResponse, error)

	// Status
	DisConnected() error
	Connected() error
	GetConnStatus() string
}

type SignalClient struct {
	sysInfo   system.SysInfo
	negClient negotiation.NegotiationServiceClient
	rtcClient rtc.RtcServiceClient
	conn      *grpc.ClientConn

	ctx context.Context

	mux sync.Mutex

	connState *conn.ConnectState

	runelog *runelog.Runelog
}

func NewSignalClient(
	sysInfo system.SysInfo,
	conn *grpc.ClientConn,
	cs *conn.ConnectState,
	runelog *runelog.Runelog,
) SignalClientImpl {
	return &SignalClient{
		sysInfo:   sysInfo,
		negClient: negotiation.NewNegotiationServiceClient(conn),
		rtcClient: rtc.NewRtcServiceClient(conn),
		conn:      conn,
		ctx:       context.Background(),
		mux:       sync.Mutex{},
		// at this time, it is in an absolutely DISCONNECTED state
		connState: cs,
		runelog:   runelog,
	}
}

func (c *SignalClient) Candidate(dstnk, srcnk string, candidate ice.Candidate) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &negotiation.CandidateRequest{
		DstNodeKey: dstnk,
		SrcNodeKey: srcnk,
		Candidate:  candidate.Marshal(),
	}

	_, err := c.negClient.Candidate(ctx, msg)
	if err != nil {
		return err
	}
	return nil
}

func (c *SignalClient) Offer(
	dstnk, srcnk string,
	uFlag string,
	pwd string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &negotiation.HandshakeRequest{
		DstNodeKey: dstnk,
		SrcNodeKey: srcnk,
		UFlag:      uFlag,
		Pwd:        pwd,
	}
	_, err := c.negClient.Offer(ctx, msg)
	if err != nil {
		return err
	}
	return nil
}

func (c *SignalClient) Answer(
	dstnk, srcnk string,
	uFlag string,
	pwd string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &negotiation.HandshakeRequest{
		DstNodeKey: dstnk,
		SrcNodeKey: srcnk,
		UFlag:      uFlag,
		Pwd:        pwd,
	}
	_, err := c.negClient.Answer(ctx, msg)
	if err != nil {
		return err
	}
	return nil
}

func (c *SignalClient) Connect(mk string, handler func(msg *negotiation.NegotiationRequest) error) error {
	md := metadata.New(map[string]string{utils.NodeKey: mk, utils.HostName: c.sysInfo.Hostname, utils.OS: c.sysInfo.OS})
	ctx := metadata.NewOutgoingContext(c.ctx, md)

	stream, err := c.negClient.Connect(ctx, grpc.WaitForReady(true))
	if err != nil {
		return err
	}

	// set connState to Connected
	c.connState.Connected()

	defer func() {
		err := stream.CloseSend()
		if err != nil {
			c.runelog.Logger.Errorf("failed to close start connect")
			return
		}
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			c.runelog.Logger.Errorf("connect stream return to EOF, received by [%s]", mk)
			return err
		}
		if err != nil {
			c.runelog.Logger.Errorf("failed to get grpc client stream for machinek key: %s", msg.DstNodeKey)
			return err
		}

		err = handler(msg)
		if err != nil {
			c.runelog.Logger.Errorf("failed to handle grpc client stream stream in machine key: %s", msg.DstNodeKey)
			return err
		}
	}
}

// function to wait until connState becomes Connected until it can
func (c *SignalClient) WaitStartConnect() {
	if c.connState.IsConnected() {
		return
	}

	ch := c.connState.GetConnChan()
	select {
	case <-c.ctx.Done():
	case <-ch:
	}
}

func (c *SignalClient) IsReady() bool {
	return c.conn.GetState() == connectivity.Ready || c.conn.GetState() == connectivity.Idle
}

func (c *SignalClient) GetStunTurnConfig() (*rtc.GetStunTurnConfigResponse, error) {
	conf, err := c.rtcClient.GetStunTurnConfig(c.ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	return conf, nil
}

func (c *SignalClient) DisConnected() error {
	c.connState.DisConnected()
	return nil
}

func (c *SignalClient) Connected() error {
	c.connState.Connected()
	return nil
}

func (c *SignalClient) GetConnStatus() string {
	c.connState.Connected()
	status := c.connState.GetConnStatus()
	return status.String()
}
