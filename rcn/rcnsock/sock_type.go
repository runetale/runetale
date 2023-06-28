// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package rcnsock

// TODO: (shinta) is this safe?
// appropriate permission and feel it would be better to
// have a process that creates a file
const sockaddr = "/tmp/rcn.sock"

type socketMessageType int

const (
	CompletedConn socketMessageType = 0
)

type DialRunetaleStatus struct {
	Ip     string
	Cidr   string
	Status string
}

type RcnDialSock struct {
	MessageType socketMessageType

	DialRunetaleStatus *DialRunetaleStatus
}
