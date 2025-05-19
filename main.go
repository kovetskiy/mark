package main

import (
	"context"
	"os"

	"github.com/kovetskiy/mark/util"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v3"
)

const (
	version     = "12.2.0"
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`
)

func main() {
	cmd := &cli.Command{
		Name:        "mark",
		Usage:       usage,
		Description: description,
		Version:     version,
		Flags:       util.Flags,
		EnableShellCompletion: true,
		HideHelpCommand:       true,
		Action:                util.RunMark,
	}

	if err := cmd.Run(context.TODO(), os.Args); err != nil {
		log.Fatal(err)
	}
}
