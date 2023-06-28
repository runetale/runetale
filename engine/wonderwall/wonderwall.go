// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package wonderwall

// wonderwall is a magic wall that coordinates communication with
// runs on runetaled daemon, exchanges status with RCN control plane with server
// so in most cases you can get information about the runetaled daemon through the server from here.

import (
	"fmt"
	"sync"

	"github.com/runetale/runetale/rcn/rcnsock"
	"github.com/runetale/runetale/runelog"
)

type WonderWall struct {
	sock *rcnsock.RcnSock

	mu *sync.Mutex

	runelog *runelog.runelog
}

func NewWonderWall(
	sock *rcnsock.RcnSock,
	runelog *runelog.runelog,
) *WonderWall {
	return &WonderWall{
		sock:    sock,
		mu:      &sync.Mutex{},
		runelog: runelog,
	}
}

func (w *WonderWall) dialRunetaleStatus() error {
	ds, err := w.sock.DialRunetaleStatus()
	if err != nil {
		return err
	}

	fmt.Printf("runetale connect to server status => [%s]\n", ds.Status)
	fmt.Printf("runetale ip => [%s/%s]\n", ds.Ip, ds.Cidr)

	return nil
}

func (w *WonderWall) Start() {
	err := w.dialRunetaleStatus()
	if err != nil {
		w.runelog.Logger.Errorf("failed to dial rcn sock %s", err.Error())
	}
}

func (w *WonderWall) Stop() {

}
