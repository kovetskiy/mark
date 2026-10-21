package util

import (
	"context"

	"github.com/urfave/cli/v3"
)

// legacyConfig is the configuration file Flags reads. A single command has no
// settle, and knows the file before it resolves any flag.
var legacyConfig = &configFile{path: new(string), settled: true}

// Flags are mark's flags, global and publish's together, as one command takes
// them -- mark's command line before there were commands:
//
//	&cli.Command{Flags: util.Flags, Before: util.CheckFlags, Action: util.RunMark}
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
