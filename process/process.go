// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package process

import "github.com/runetale/runetale/runelog"

type Process interface {
	GetRunetaledProcess() bool
}

func NewProcess(
	runelog *runelog.runelog,
) Process {
	return newProcess(runelog)
}
