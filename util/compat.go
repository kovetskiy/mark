package util

import (
	"context"

	"github.com/urfave/cli/v3"
)

// legacyConfig is the configuration file Flags reads.
var legacyConfig = &configFile{path: new(string)}

// Flags are mark's flags, global and publish's together, as one command takes
// them -- mark's command line before there were commands:
//
//	&cli.Command{Flags: util.Flags, Before: util.CheckFlags, Action: util.RunMark}
//
// Every flag reads the configuration file the "config" flag names, which on a
// single command is known before any of them is resolved.
//
// Deprecated: use Run, which is mark's command line, commands and all.
var Flags = append(globalFlags(legacyConfig), publishFlags(legacyConfig)...)

// RunMark publishes, as the action of a command built from Flags, with the
// logging its flags ask for.
//
// Deprecated: use Run.
func RunMark(ctx context.Context, cmd *cli.Command) error {
	if err := SetUpLogging(cmd); err != nil {
		return err
	}

	return RunPublish(ctx, cmd)
}
