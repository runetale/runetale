// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/peterbourgon/ff/v2/ffcli"
)

var version = "dev"

var versionCmd = &ffcli.Command{
	Name:       "version",
	ShortUsage: "version",
	ShortHelp:  "Show runetale Version",
	Exec:       execVersion,
}

func execVersion(ctx context.Context, args []string) error {
	if len(args) > 0 {
		log.Fatalf("too many arugments: %q", args)
	}

	fmt.Println(version)

	return nil
}
