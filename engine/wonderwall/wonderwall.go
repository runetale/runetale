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

	runelog *runelog.Runelog
}

func NewWonderWall(
	sock *rcnsock.RcnSock,
	runelog *runelog.Runelog,
) *WonderWall {
	return &WonderWall{
		sock:    sock,
		mu:      &sync.Mutex{},
		runelog: runelog,
	}
}

func (w *WonderWall) dialRunetaleStatus() error {
	ds, err := w.sock.DialRunetaledStatus()
	if err != nil {
		return err
	}

	w.runelog.Logger.Debugf("runetale connect to signal server status => [%s]", ds.ConnStatus)
	w.runelog.Logger.Debugf("runetale ip => [%s/%s]", ds.Ip, ds.Cidr)

	return nil
}

func (w *WonderWall) Start() {
	err := w.dialRunetaleStatus()
	if err != nil {
		w.runelog.Logger.Debugf("failed to dial rcn sock %s", err.Error())
		fmt.Println("please retry runetaled up")
	}
}

func (w *WonderWall) Stop() {

}
