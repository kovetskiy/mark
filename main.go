package main

import (
	"os"

	"github.com/kovetskiy/mark/util"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const (
	version     = "12.2.0"
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`
)

func main() {
	app := &cli.App{
		Name:        "mark",
		Usage:       usage,
		Description: description,
		Version:     version,
		Flags:       util.Flags,
		Before: altsrc.InitInputSourceWithContext(util.Flags,
			func(context *cli.Context) (altsrc.InputSourceContext, error) {
				if context.IsSet("config") {
					filePath := context.String("config")
					return altsrc.NewTomlSourceFromFile(filePath)
				} else {
					// Fall back to default if config is unset and path exists
					_, err := os.Stat(util.ConfigFilePath())
					if os.IsNotExist(err) {
						return &altsrc.MapInputSource{}, nil
					}
					return altsrc.NewTomlSourceFromFile(util.ConfigFilePath())
				}
			}),
		EnableBashCompletion: true,
		HideHelpCommand:      true,
		Action:               util.RunMark,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
