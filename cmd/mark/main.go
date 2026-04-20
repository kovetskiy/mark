package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kovetskiy/mark/v16/util"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

var (
	version = "dev"
	commit  = "none"
)

const (
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`
)

func main() {
	cmd := &cli.Command{
		Name:                  "mark",
		Usage:                 usage,
		Description:           description,
		Version:               fmt.Sprintf("%s@%s", version, commit),
		Flags:                 util.Flags,
		EnableShellCompletion: true,
		HideHelpCommand:       true,
		Before:                util.CheckFlags,
		Action:                util.RunMark,
	}

	if err := cmd.Run(context.TODO(), os.Args); err != nil {
		log.Fatal().Msg(err.Error())
	}
}
