// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package webrtc

type Credentials struct {
	UserName string
	Pwd      string
}

func NewCredentials(uname, pwd string) *Credentials {
	return &Credentials{
		UserName: uname,
		Pwd:      pwd,
	}
}
