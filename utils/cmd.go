// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package utils

import (
	"os/exec"
	"strings"
)

func ExecCmd(command string) (string, error) {
	args := strings.Fields(command)
	out, err := exec.Command(args[0], args[1:]...).Output()
	return string(out), err
}
