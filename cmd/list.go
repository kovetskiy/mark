package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/auth"
	"github.com/kovetskiy/mark/confluence"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var listFlags = []cli.Flag{
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "output-format",
		Aliases: []string{"o"},
		Value:   "table",
		Usage:   "Defines output format (json or table)",
		EnvVars: []string{"MARK_SPACE"},
	})}

var ListCmd = &cli.Command{
	Name:  "list",
	Usage: "lists pages, spaces and labels",
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
			Action: ListPages,
		},
		{
			Name:   "spaces",
			Usage:  "lists spaces.",
			Flags:  append(flags, listFlags...),
			Action: ListSpaces,
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

func ListPages(cCtx *cli.Context) error {

	if cCtx.Bool("debug") {
		log.SetLevel(lorg.LevelDebug)
	}

	if cCtx.Bool("trace") {
		log.SetLevel(lorg.LevelTrace)
	}

	if cCtx.String("color") == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	return nil
}

func ListSpaces(cCtx *cli.Context) error {

	if cCtx.Bool("debug") {
		log.SetLevel(lorg.LevelDebug)
	}

	if cCtx.Bool("trace") {
		log.SetLevel(lorg.LevelTrace)
	}

	if cCtx.String("color") == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	creds, err := auth.GetCredentials(cCtx.String("username"), cCtx.String("password"), "", cCtx.String("base-url"), false)
	if err != nil {
		return err
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	spaces, err := api.ListSpaces()

	if err != nil {
		log.Fatal(err)
		os.Exit(0)
	}

	if cCtx.String("output-format") == "json" {
		s, _ := json.MarshalIndent(spaces.Spaces, "", "\t")
		fmt.Print(string(s))
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
		fmt.Fprintln(w, "ID\tKey\tName")
		for _, space := range spaces.Spaces {
			fmt.Fprintf(w, "%s\t%s\t%d\n", space.Key, space.Name, space.ID)
		}
		w.Flush()
	}

	return nil
}

func ListLabels(cCtx *cli.Context) error {

	if cCtx.Bool("debug") {
		log.SetLevel(lorg.LevelDebug)
	}

	if cCtx.Bool("trace") {
		log.SetLevel(lorg.LevelTrace)
	}

	if cCtx.String("color") == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	return nil
}
