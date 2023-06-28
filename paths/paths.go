// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// state file to manage the secret information of the server.
// do not disclose to the outside world.
func DefaultRunetaleClientStateFile() string {
	switch runtime.GOOS {
	case "freebsd", "openbsd":
		return "/var/db/runetale/client.state"
	case "linux":
		return "/var/lib/runetale/client.state"
	case "darwin":
		return "/Library/runetale/client.state"
	default:
		return ""
	}
}

func DefaultClientConfigFile() string {
	return "/etc/runetale/client.json"
}

func DefaultClientLogFile() string {
	return "/var/log/runetale/client.log"
}

func DefaultRunetaledLogFile() string {
	return "var/log/runetaled/client.log"
}

func MkStateDir(dirPath string) error {
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return err
	}

	return checkStateDirPermission(dirPath)
}

func checkStateDirPermission(dir string) error {
	const (
		perm = 700
	)

	if filepath.Base(dir) != "runetale" {
		return nil
	}

	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}

	if !fi.IsDir() {
		return fmt.Errorf("expected %q is a directory, but %v", dir, fi.Mode())
	}

	if fi.Mode().Perm() == perm {
		return nil
	}

	return os.Chmod(dir, perm)
}
