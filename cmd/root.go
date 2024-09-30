package cmd

import (
	"os"

	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const (
	version     = "11.1.0"
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`
)

var flags = []cli.Flag{
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "dry-run",
		Value:   false,
		Usage:   "resolve page and ancestry, show resulting HTML and exit.",
		EnvVars: []string{"MARK_DRY_RUN"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "color",
		Value:   "auto",
		Usage:   "display logs in color. Possible values: auto, never.",
		EnvVars: []string{"MARK_COLOR"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "debug",
		Value:   false,
		Usage:   "enable debug logs.",
		EnvVars: []string{"MARK_DEBUG"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "trace",
		Value:   false,
		Usage:   "enable trace logs.",
		EnvVars: []string{"MARK_TRACE"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "username",
		Aliases: []string{"u"},
		Value:   "",
		Usage:   "use specified username for updating Confluence page.",
		EnvVars: []string{"MARK_USERNAME"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "password",
		Aliases: []string{"p"},
		Value:   "",
		Usage:   "use specified token for updating Confluence page. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication.",
		EnvVars: []string{"MARK_PASSWORD"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "base-url",
		Aliases: []string{"b", "base_url"},
		Value:   "",
		Usage:   "base URL for Confluence. Alternative option for base_url config field.",
		EnvVars: []string{"MARK_BASE_URL"},
	}),
	&cli.StringFlag{
		Name:      "config",
		Aliases:   []string{"c"},
		Value:     configFilePath(),
		Usage:     "use the specified configuration file.",
		TakesFile: true,
		EnvVars:   []string{"MARK_CONFIG"},
	}}

func Exec() {
	app := &cli.App{
		Name:                 "mark",
		Usage:                usage,
		Description:          description,
		Version:              version,
		EnableBashCompletion: true,
		HideHelpCommand:      true,
		Commands: []*cli.Command{
			PublishCmd,
			ListCmd,
		},
	}

	app.Before = altsrc.InitInputSourceWithContext(app.Flags,
		func(context *cli.Context) (altsrc.InputSourceContext, error) {
			if context.IsSet("config") {
				filePath := context.String("config")
				return altsrc.NewTomlSourceFromFile(filePath)
			} else {
				// Fall back to default if config is unset and path exists
				_, err := os.Stat(configFilePath())
				if os.IsNotExist(err) {
					return &altsrc.MapInputSource{}, nil
				}
				return altsrc.NewTomlSourceFromFile(configFilePath())
			}
		})

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
