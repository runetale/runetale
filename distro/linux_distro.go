// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package distro

import (
	"os"
	"runtime"
)

type Distribution string

const (
	NixOS = Distribution("nixos")
)

func Get() Distribution {
	if runtime.GOOS == "linux" {
		return linuxDistro()
	}
	return ""
}

func checkDistro(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

func linuxDistro() Distribution {
	switch {
	case checkDistro("/run/current-system/sw/bin/nixos-version"):
		return NixOS
	}
	return ""
}
