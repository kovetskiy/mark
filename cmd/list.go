package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var ListCmd = &cli.Command{
	Name:  "list",
	Usage: "lists pages, spaces and labels",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		{
			Name:  "pages",
			Usage: "lists pages.",
			Flags: []cli.Flag{
				altsrc.NewStringFlag(&cli.StringFlag{
					Name:    "space",
					Value:   "",
					Usage:   "lists all pages in a space.",
					EnvVars: []string{"MARK_SPACE"},
				}),
			},
			Action: ListSpaces,
		},
		{
			Name:  "spaces",
			Usage: "lists spaces.",
			Action: func(cCtx *cli.Context) error {
				fmt.Println("removed task template: ", cCtx.Args().First())
				return nil
			},
		},
		{
			Name:  "labels",
			Usage: "lists labels.",
			Action: func(cCtx *cli.Context) error {
				fmt.Println("removed task template: ", cCtx.Args().First())
				return nil
			},
		},
	},
}

func ListSpaces(cCtx *cli.Context) error {

}
