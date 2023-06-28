// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"strings"

	"github.com/peterbourgon/ff/v2/ffcli"
)

func Run(args []string) error {
	if len(args) == 1 && (args[0] == "-V" || args[0] == "--version" || args[0] == "-v") {
		args = []string{"version"}
	}

	fs := flag.NewFlagSet("runetale", flag.ExitOnError)

	cmd := &ffcli.Command{
		Name:       "runetale",
		ShortUsage: "runetale <subcommands> [command flags]",
		ShortHelp:  "Use WireGuard for easy and secure private connections.",
		LongHelp: strings.TrimSpace(`
All flags can use a single or double hyphen.

For help on subcommands, prefix with -help.

Flags and options are subject to change.
`),
		Subcommands: []*ffcli.Command{
			upCmd,
			loginCmd,
			versionCmd,
		},
		FlagSet: fs,
		Exec:    func(context.Context, []string) error { return flag.ErrHelp },
	}

	if err := cmd.Parse(args); err != nil {
		return err
	}

	if err := cmd.Run(context.Background()); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	return nil
}
