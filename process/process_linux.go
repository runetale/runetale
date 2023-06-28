// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package process

import (
	"os/exec"

	"github.com/runetale/runetale/runelog"
)

type runetaledProcessOnLinux struct {
	runelog *runelog.runelog
}

func newProcess(
	runelog *runelog.runelog,
) Process {
	return &runetaledProcessOnLinux{
		runelog: runelog,
	}
}

func (d *runetaledProcessOnLinux) GetRunetaledProcess() bool {
	cmd := exec.Command("pgrep", "runetaled")
	if out, err := cmd.CombinedOutput(); err != nil {
		d.runelog.Logger.Errorf("Command: %v failed with output %s and error: %v", cmd.String(), out, err)
		return false
	}
	return true
}
