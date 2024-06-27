// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

import (
	"fmt"
	"os"

	"github.com/runetale/runetale/log"
)

type upstartRecord struct {
	// binary path
	binPath string
	// daemon name
	serviceName string
	// daemon file path
	daemonFilePath string
	// daemon system config
	systemConfig string

	log *log.Logger
}

func (d *upstartRecord) Install() (err error) {
	defer func() {
		if os.Getuid() != 0 && err != nil {
			d.log.Logger.Errorf("run it again with sudo privileges: %s", err.Error())
			err = fmt.Errorf("run it again with sudo privileges: %s", err.Error())
		}
	}()

	return nil
}

func (d *upstartRecord) Uninstall() error {
	return nil
}

func (d *upstartRecord) Load() error {
	return nil
}

func (d *upstartRecord) Unload() error {
	return nil
}

func (d *upstartRecord) Start() error {
	return nil
}

func (d *upstartRecord) Stop() error {
	return nil
}

func (d *upstartRecord) Status() (string, bool) {
	return "", false
}

func (d *upstartRecord) IsInstalled() bool {
	return false
}

func (d *upstartRecord) IsRunnning() (string, bool) {
	return "", false
}
