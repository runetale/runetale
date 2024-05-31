// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package rcn

// rcn package is realtime communication nucleus
// provides communication status and P2P communication aids
// you must be logged in to use it
//

import (
	"sync"

	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/conf"
	"github.com/runetale/runetale/iface"
	"github.com/runetale/runetale/rcn/controlplane"
	"github.com/runetale/runetale/rcn/rcnsock"
	"github.com/runetale/runetale/runelog"
)

type Rcn struct {
	cp           *controlplane.ControlPlane
	serverClient grpc.ServerClientImpl
	conf         *conf.Conf
	iface        *iface.Iface
	mk           string
	mu           *sync.Mutex
	runelog      *runelog.Runelog
}

func NewRcn(
	conf *conf.Conf,
	mk string,
	ch chan struct{},
	runelog *runelog.Runelog,
) *Rcn {

	cp := controlplane.NewControlPlane(
		conf.SignalClient,
		conf.ServerClient,
		rcnsock.NewRcnSock(runelog, ch),
		mk,
		conf,
		ch,
		runelog,
	)

	return &Rcn{
		cp:           cp,
		serverClient: conf.ServerClient,
		conf:         conf,
		mk:           mk,
		mu:           &sync.Mutex{},
		runelog:      runelog,
	}
}

// TODO(snt): also set up a grpc server to talk to cli?
// call Setup function before Start
func (r *Rcn) Setup(ip, cidr string) error {
	err := r.createIface(ip, cidr)
	if err != nil {
		return err
	}

	return nil
}

func (r *Rcn) Start() {
	err := r.cp.ConfigureStunTurnConf()
	if err != nil {
		r.runelog.Logger.Errorf("failed to set up puncher, %s", err.Error())
	}

	go r.cp.WaitForRemoteConn()

	go r.cp.ConnectSignalServer()

	go r.cp.ConnectSock(r.iface.IP, r.iface.CIDR)

	r.runelog.Logger.Debugf("started rcn")
}

func (r *Rcn) createIface(ip, cidr string) error {
	r.iface = iface.NewIface(r.conf.Spec.TunName, r.conf.Spec.WgPrivateKey, ip, cidr, r.runelog)
	return iface.CreateIface(r.iface, r.runelog)
}

func (r *Rcn) Stop() error {
	err := r.cp.Close()
	if err != nil {
		r.runelog.Logger.Errorf("failed to close control plane, because %s", err.Error())
		return err
	}

	err = iface.RemoveIface(r.iface.Tun, r.runelog)
	if err != nil {
		r.runelog.Logger.Errorf("failed to remove iface, because %s", err.Error())
		return err
	}

	r.runelog.Logger.Debugf("closed complete rcn")
	return err
}
