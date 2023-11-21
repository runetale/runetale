// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

const (
	running      = "\t[\033[32mRUNNING\033[0m]"
	failed       = "\t[\033[31mFAILED\033[0m]"
	nonroot      = "\t[\033[31mNON_ROOT\033[0m]"
	notinstalled = "\t[\033[31mNOT_INSTALLED\033[0m]"
	notrunning   = "\t[\033[31mNOT_RUNNING\033[0m]"
)

type Daemon interface {
	Install() error
	Uninstall() error
	Status() (string, bool)
}

func NewDaemon(
	binPath, serviceName, daemonFilePath, systemConfig string,
	runelog *runelog.runelog,
) Daemon {
	return newDaemon(binPath, serviceName, daemonFilePath, systemConfig, runelog)
}
