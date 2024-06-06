// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"sync"

	"github.com/runetale/runetale/client/grpc"
	"github.com/runetale/runetale/engine/wonderwall"
	"github.com/runetale/runetale/rcn/rcnsock"
	"github.com/runetale/runetale/runelog"
	"github.com/runetale/runetale/wg"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type engine struct {
	runelog *runelog.Runelog

	nk        string
	tunName   string
	ip        string
	cidr      string
	wgPrivKey string
	wgPort    int
	blackList []string

	sock *rcnsock.RcnSock
	ww   *wonderwall.WonderWall

	ctx    context.Context
	cancel context.CancelFunc

	mu *sync.Mutex

	rootch chan struct{}
}

func NewEngine(
	serverClient grpc.ServerClientImpl,
	runelog *runelog.Runelog,
	tunName string,
	nk string,
	ip string,
	cidr string,
	wgPrivKey string,
	blackList []string,
	ctx context.Context,
	cancel context.CancelFunc,
) (*engine, error) {
	_, err := wgtypes.ParseKey(wgPrivKey)
	if err != nil {
		return nil, err
	}

	ch := make(chan struct{})
	mu := &sync.Mutex{}

	sock := rcnsock.NewRcnSock(runelog, ch)
	ww := wonderwall.NewWonderWall(sock, runelog, ch)

	return &engine{
		runelog: runelog,

		nk:        nk,
		tunName:   tunName,
		ip:        ip,
		cidr:      cidr,
		wgPrivKey: wgPrivKey,
		wgPort:    wg.WgPort,
		blackList: blackList,

		sock: sock,
		ww:   ww,

		ctx:    ctx,
		cancel: cancel,

		mu: mu,

		rootch: ch,
	}, nil
}

func (d *engine) startWonderWall() {
	d.ww.Start()
}

func (d *engine) stopWonderWall() {
	d.ww.Stop()
}

func (d *engine) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.startWonderWall()

	go func() {
		// do somethings
		// system resouce check?
	}()
	<-d.rootch

	d.Stop()

	return nil
}

func (d *engine) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stopWonderWall()
}
