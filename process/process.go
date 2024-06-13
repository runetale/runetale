// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package process

import "github.com/runetale/runetale/log"

type Process interface {
	GetRunetaledProcess() bool
}

func NewProcess(
	logger *log.Logger,
) Process {
	return newProcess(logger)
}
