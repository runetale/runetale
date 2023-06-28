// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package grpc

import (
	"crypto/tls"

	"github.com/runetale/runetale/runelog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func NewGrpcDialOption(runelog *runelog.runelog, isdebug bool) grpc.DialOption {
	var option grpc.DialOption
	if isdebug {
		option = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		tlsCredentials := credentials.NewTLS(&tls.Config{})
		option = grpc.WithTransportCredentials(tlsCredentials)
	}
	return option
}
