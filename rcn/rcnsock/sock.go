// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package rcnsock

// a package that communicates using rcn and unix sockets
//

import (
	"encoding/gob"
	"net"
	"os"

	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/runelog"
)

type RcnSock struct {
	signalClient grpc.SignalClientImpl

	ip   string
	cidr string

	runelog *runelog.Runelog

	ch chan struct{}
}

// if scp is nil when making this function call, just listen
func NewRcnSock(
	runelog *runelog.Runelog,
	ch chan struct{},
) *RcnSock {
	return &RcnSock{

		runelog: runelog,

		ch: ch,
	}
}

func (s *RcnSock) cleanup() error {
	if _, err := os.Stat(sockaddr); err == nil {
		if err := os.RemoveAll(sockaddr); err != nil {
			return err
		}
	}
	return nil
}

func (s *RcnSock) listen(conn net.Conn) {
	defer conn.Close()

	decoder := gob.NewDecoder(conn)
	encoder := gob.NewEncoder(conn)

	for {
		mes := &RcnDialSock{}
		err := decoder.Decode(mes)
		if err != nil {
			break
		}

		switch mes.MessageType {
		case CompletedConn:
			status := s.signalClient.GetConnStatus()
			mes.DialRunetaleStatus.Ip = s.ip
			mes.DialRunetaleStatus.Cidr = s.cidr
			mes.DialRunetaleStatus.Status = status
		}

		err = encoder.Encode(mes)
		if err != nil {
			s.runelog.Logger.Errorf("failed to encode wondersock. %s", err.Error())
			break
		}
	}
}

func (s *RcnSock) Connect(
	signalClient grpc.SignalClientImpl,
	ip, cidr string,
) error {
	err := s.cleanup()
	if err != nil {
		return err
	}

	s.ip = ip
	s.cidr = cidr
	s.signalClient = signalClient

	listener, err := net.Listen("unix", sockaddr)
	if err != nil {
		return err
	}

	go func() {
		<-s.ch
		s.runelog.Logger.Debugf("close the rcn socket")
		s.cleanup()
	}()

	s.runelog.Logger.Debugf("starting rcn socket")
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.runelog.Logger.Errorf("failed to accept rcn socket. %s", err.Error())
		}

		s.runelog.Logger.Debugf("accepted rcn sock")

		go s.listen(conn)
	}
}

func (s *RcnSock) DialRunetaleStatus() (*DialRunetaleStatus, error) {
	conn, err := net.Dial("unix", sockaddr)
	defer conn.Close()
	if err != nil {
		return nil, err
	}

	decoder := gob.NewDecoder(conn)
	encoder := gob.NewEncoder(conn)

	d := &RcnDialSock{
		MessageType:        CompletedConn,
		DialRunetaleStatus: &DialRunetaleStatus{},
	}

	err = encoder.Encode(d)
	if err != nil {
		return nil, err
	}

	err = decoder.Decode(d)
	if err != nil {
		return nil, err
	}

	return d.DialRunetaleStatus, nil
}
