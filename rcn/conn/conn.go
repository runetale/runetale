// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package conn

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/ice/v2"
	"github.com/runetale/runetale/rcn/proxy"
	"github.com/runetale/runetale/runelog"
)

type Conn struct {
	agent      *ice.Agent
	remoteConn *ice.Conn
	uname      string
	pwd        string

	wireproxy *proxy.WireProxy

	remoteWgPubKey string
	wgPubKey       string

	ctx    context.Context
	cancel context.CancelFunc

	mu *sync.Mutex

	runelog *runelog.Runelog
}

func NewConn(
	agent *ice.Agent,
	uname string,
	pwd string,

	wireproxy *proxy.WireProxy,

	remoteWgPubKey string,
	wgPubKey string,

	runelog *runelog.Runelog,
) *Conn {
	ctx, cancel := context.WithCancel(context.Background())

	return &Conn{
		agent: agent,
		uname: uname,
		pwd:   pwd,

		wireproxy: wireproxy,

		remoteWgPubKey: remoteWgPubKey,
		wgPubKey:       wgPubKey,

		ctx:    ctx,
		cancel: cancel,

		mu: &sync.Mutex{},

		runelog: runelog,
	}
}

func (c *Conn) Start() error {
	var err error
	fmt.Println(c.wgPubKey)
	fmt.Println(c.remoteWgPubKey)
	if c.wgPubKey < c.remoteWgPubKey {
		c.remoteConn, err = c.agent.Dial(c.ctx, c.uname, c.pwd)
		if err != nil {
			c.runelog.Logger.Errorf("failed to dial agent")
			return err
		}
		c.runelog.Logger.Infof("completed dial agent")
	} else {
		c.remoteConn, err = c.agent.Accept(c.ctx, c.uname, c.pwd)
		if err != nil {
			c.runelog.Logger.Errorf("failed to accept agent")
			return err
		}
		c.runelog.Logger.Infof("completed accept agent")
	}

	err = c.wireproxy.StartProxy(c.remoteConn)
	if err != nil {
		c.runelog.Logger.Errorf("failed to start proxy, %s", err.Error())
		return err
	}

	c.runelog.Logger.Infof("completed p2p connection, local: [%s] <-> remote: [%s]", c.wgPubKey, c.remoteWgPubKey)

	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.wireproxy == nil {
		return nil
	}

	err := c.wireproxy.Stop()
	if err != nil {
		c.runelog.Logger.Errorf("failed to stop wireproxy")
		return err
	}

	// close the ice agent connection
	c.cancel()

	c.runelog.Logger.Debugf("close conn")

	return nil
}
