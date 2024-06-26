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
	info := getInfo()
	for strings.Contains(info, "broken pipe") {
		info = getInfo()
		time.Sleep(500 * time.Millisecond)
	}

	releaseInfo := getReleaseInfo()
	for strings.Contains(info, "broken pipe") {
		releaseInfo = getReleaseInfo()
		time.Sleep(500 * time.Millisecond)
	}

	osRelease := strings.Split(releaseInfo, "\n")
	var osName string
	var osVer string
	for _, s := range osRelease {
		if strings.HasPrefix(s, "NAME=") {
			osName = strings.Split(s, "=")[1]
			osName = strings.ReplaceAll(osName, "\"", "")
		} else if strings.HasPrefix(s, "VERSION_ID=") {
			osVer = strings.Split(s, "=")[1]
			osVer = strings.ReplaceAll(osVer, "\"", "")
		}
	}

	osStr := strings.Replace(info, "\n", "", -1)
	osStr = strings.Replace(osStr, "\r\n", "", -1)
	osInfo := strings.Split(osStr, " ")
	if osName == "" {
		osName = osInfo[3]
	}
	gio := &SysInfo{
		Kernel:    osInfo[0],
		Core:      osInfo[1],
		Platform:  osInfo[2],
		OS:        osName,
		OSVersion: osVer,
		GoOS:      runtime.GOOS,
		CPUs:      runtime.NumCPU(),
	}
	gio.Hostname, _ = os.Hostname()

	return gio
}

func getInfo() string {
	cmd := exec.Command("uname", "-srio")
	cmd.Stdin = strings.NewReader("some")
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

func getReleaseInfo() string {
	cmd := exec.Command("cat", "/etc/os-release")
	cmd.Stdin = strings.NewReader("some")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println("getReleaseInfo:", err)
	}
	return out.String()
}
