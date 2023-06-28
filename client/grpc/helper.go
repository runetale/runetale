// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"google.golang.org/grpc/metadata"
)

func getLoginSessionID(md metadata.MD) string {
	registered := md.Get("session_id")
	return registered[0]
}
