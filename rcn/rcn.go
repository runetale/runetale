// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package rcn

// rcn package is realtime communication nucleus
// provides communication status and P2P communication aids
// you must be logged in to use it
//

import (
	"encoding/json"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync"
	"time"

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
	nk           string
	mu           *sync.Mutex
	ch           chan struct{}
	runelog      *runelog.Runelog
	debug        bool
}

func NewRcn(
	conf *conf.Conf,
	nk string,
	ch chan struct{},
	runelog *runelog.Runelog,
	debug bool,
) *Rcn {

	cp := controlplane.NewControlPlane(
		conf.SignalClient,
		conf.ServerClient,
		rcnsock.NewRcnSock(runelog, ch),
		nk,
		conf,
		ch,
		runelog,
	)

	return &Rcn{
		cp:           cp,
		serverClient: conf.ServerClient,
		conf:         conf,
		nk:           nk,
		mu:           &sync.Mutex{},
		ch:           ch,
		runelog:      runelog,
		debug:        debug,
	}
}

// required root permission
func (r *Rcn) Setup(ip, cidr string) error {
	err := r.createIface(ip, cidr)
	if err != nil {
		return err
	}

	return nil
}

func (r *Rcn) Start() {
	go r.cp.WaitForRemoteConn()

	err := r.cp.ConfigureStunTurnConf()
	if err != nil {
		r.runelog.Logger.Errorf("failed to set up relay server, %s", err.Error())
	}

	err = r.cp.SyncRemoteNodes()
	if err != nil {
		r.runelog.Logger.Errorf("failed to get remote nodes, %s", err.Error())
		close(r.ch)
		return
	}

	go r.cp.ConnectSignalServer()

	err = r.cp.Join()
	if err != nil {
		r.runelog.Logger.Errorf("failed to join network, %s", err.Error())
		close(r.ch)
		return
	}

	go r.cp.ConnectSock(r.iface.IP, r.iface.CIDR)

	r.systemMonitor()

	r.runelog.Logger.Debugf("started rcn")
}

func (r *Rcn) systemMonitor() {
	if r.debug {
		go func() {
			http.ListenAndServe("localhost:6060", nil)
			m := struct {
				Alloc,
				TotalAlloc,
				Sys,
				Mallocs,
				Frees,
				LiveObjects,
				PauseTotalNs uint64

				NumGC        uint32
				NumGoroutine int
			}{}

			var rtm runtime.MemStats
			for {
				select {
				case _ = <-time.NewTicker(10000 * time.Millisecond).C:
					runtime.ReadMemStats(&rtm)

					m.NumGoroutine = runtime.NumGoroutine()
					m.Alloc = rtm.Alloc
					m.TotalAlloc = rtm.TotalAlloc
					m.Sys = rtm.Sys
					m.Mallocs = rtm.Mallocs
					m.Frees = rtm.Frees
					m.LiveObjects = m.Mallocs - m.Frees
					m.PauseTotalNs = rtm.PauseTotalNs
					m.NumGC = rtm.NumGC

					b, _ := json.Marshal(m)
					r.runelog.Logger.Debugf(string(b))
				}
			}
		}()
	}
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
