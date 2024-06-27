// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package process

import (
	"log"
	"os/exec"
)

type runetaledProcessOnLinux struct {
	logger *log.Logger
}

func newProcess(
	logger *log.Logger,
) Process {
	return &runetaledProcessOnLinux{
		logger: logger,
	}
}

func (d *runetaledProcessOnLinux) GetRunetaledProcess() bool {
	cmd := exec.Command("pgrep", "runetaled")
	if out, err := cmd.CombinedOutput(); err != nil {
		d.log.log.Errorf("Command: %v failed with output %s and error: %v", cmd.String(), out, err)
		return false
	}
	return true
}
