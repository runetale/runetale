// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package system

type SysInfo struct {
	GoOS      string
	Kernel    string
	Core      string
	Platform  string
	OS        string
	OSVersion string
	Hostname  string
	CPUs      int
	Version   string
}

func NewSysInfo() *SysInfo {
	return GetInfo()
}
