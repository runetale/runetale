// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package rcnsock

const sockaddr = "/tmp/rcn.sock"

type socketMessageType int

const (
	CompletedConn socketMessageType = 0
)

type DialRunetaleStatus struct {
	Ip         string
	Cidr       string
	ConnStatus string
}

type RcnDialSock struct {
	MessageType socketMessageType

	DialRunetaleStatus *DialRunetaleStatus
}
