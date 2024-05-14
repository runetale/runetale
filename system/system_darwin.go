// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package system

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func GetInfo() *SysInfo {
	out := getInfo()
	for strings.Contains(out, "broken pipe") {
		out = getInfo()
		time.Sleep(500 * time.Millisecond)
	}
	osStr := strings.Replace(out, "\n", "", -1)
	osStr = strings.Replace(osStr, "\r\n", "", -1)
	osInfo := strings.Split(osStr, " ")
	gio := &SysInfo{
		Kernel:    osInfo[0],
		OSVersion: osInfo[1],
		Core:      osInfo[1],
		Platform:  osInfo[2],
		OS:        osInfo[0],
		GoOS:      runtime.GOOS,
		CPUs:      runtime.NumCPU(),
	}
	gio.Hostname, _ = os.Hostname()
	return gio
}

func getInfo() string {
	cmd := exec.Command("uname", "-srm")
	cmd.Stdin = strings.NewReader("some input")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println("getInfo:", err)
	}
	return out.String()
}
