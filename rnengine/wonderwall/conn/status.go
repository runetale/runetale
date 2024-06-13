// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package conn

import "sync"

type ConnStatus string

const Connect ConnStatus = "CONNECT"
const DisConnect ConnStatus = "DISCONNECT"

func (s ConnStatus) String() string {
	switch s {
	case Connect:
		return "connect"
	case DisConnect:
		return "disconnet"
	default:
		return "unreachable"
	}
}

type ConnectState struct {
	State ConnStatus
	conn  chan struct{}
	mu    sync.Mutex
}

func NewConnectedState() *ConnectState {
	return &ConnectState{
		State: DisConnect,
		mu:    sync.Mutex{},
	}
}

func (c *ConnectState) UpdateState(cs ConnStatus) ConnStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.State = cs
	if c.State == cs {
		return DisConnect
	}

	return c.State
}

func (c *ConnectState) IsConnected() bool {
	if c.State.String() == Connect.String() {
		return true
	}
	return false
}

func (c *ConnectState) GetConnChan() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		c.conn = make(chan struct{})
	}

	return c.conn
}

func (c *ConnectState) Connected() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.State = Connect

	if c.conn != nil {
		close(c.conn)
		c.conn = nil
	}
}

func (c *ConnectState) DisConnected() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.State = DisConnect
}

func (c *ConnectState) GetConnStatus() ConnStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.State
}
