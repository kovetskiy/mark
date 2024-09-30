package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
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

var pageFlags = []cli.Flag{
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "space",
		Value:   "",
		Usage:   "lists all pages in a space.",
		EnvVars: []string{"MARK_SPACE"},
	})}

var ListCmd = &cli.Command{
	Name:  "list",
	Usage: "lists pages, spaces and labels",
	Subcommands: []*cli.Command{
		{
			Name:   "pages",
			Usage:  "lists pages.",
			Flags:  append(append(flags, listFlags...), pageFlags...),
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

	creds, err := auth.GetCredentials(cCtx.String("username"), cCtx.String("password"), "", cCtx.String("base-url"), false)
	if err != nil {
		return err
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	pages, err := api.ListPages(cCtx.String("space"))

	if err != nil {
		log.Fatal(err)
		os.Exit(0)
	}

	if cCtx.String("output-format") == "json" {
		p, _ := json.MarshalIndent(pages.Pages, "", "\t")
		fmt.Print(string(p))
	} else {
		w := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', 0)
		fmt.Fprintln(w, "Name\tID")

		slices.SortFunc(pages.Pages, func(a, b confluence.PageInfo) int {
			return cmp.Compare(a.Title, b.Title)
		})
		for _, page := range pages.Pages {
			fmt.Fprintf(w, "%s\t%s\n", page.Title, page.ID)
		}
		w.Flush()
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
		w := tabwriter.NewWriter(os.Stdout, 6, 4, 3, ' ', 0)
		fmt.Fprintln(w, "Key\tName\tID")

		slices.SortFunc(spaces.Spaces, func(a, b confluence.SpaceInfo) int {
			return cmp.Compare(a.Key, b.Key)
		})
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
