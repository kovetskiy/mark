package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/mark"
	"github.com/kovetskiy/mark/pkg/mark/includes"
	"github.com/kovetskiy/mark/pkg/mark/macro"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

type Flags struct {
	FileGlobPatten string `docopt:"-f"`
	CompileOnly    bool   `docopt:"--compile-only"`
	DryRun         bool   `docopt:"--dry-run"`
	EditLock       bool   `docopt:"-k"`
	DropH1         bool   `docopt:"--drop-h1"`
	TitleFromH1    bool   `docopt:"--title-from-h1"`
	MinorEdit      bool   `docopt:"--minor-edit"`
	Color          string `docopt:"--color"`
	Debug          bool   `docopt:"--debug"`
	Trace          bool   `docopt:"--trace"`
	Username       string `docopt:"-u"`
	Password       string `docopt:"-p"`
	TargetURL      string `docopt:"-l"`
	BaseURL        string `docopt:"--base-url"`
	Config         string `docopt:"--config"`
	Ci             bool   `docopt:"--ci"`
	Space          string `docopt:"--space"`
}

const (
	version = "8.7"
	usage   = `mark - a tool for updating Atlassian Confluence pages from markdown.

Docs: https://github.com/kovetskiy/mark

Usage:
  mark [options] [-u <username>] [-p <token>] [-k] [-l <url>] -f <file>
  mark [options] [-u <username>] [-p <password>] [-k] [-b <url>] -f <file>
  mark -v | --version
  mark -h | --help

Options:
  -u <username>        Use specified username for updating Confluence page.
  -p <token>           Use specified token for updating Confluence page.
                        Specify - as password to read password from stdin, or your Personal access token.
                        Username is not mandatory if personal access token is provided.
                        For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication.
  -l <url>             Edit specified Confluence page.
                        If -l is not specified, file should contain metadata (see
                        above).
  -b --base-url <url>  Base URL for Confluence.
                        Alternative option for base_url config field.
  -f <file>            Use specified markdown file(s) for converting to html.
                        Supports file globbing patterns (needs to be quoted).
  -k                   Lock page editing to current user only to prevent accidental
                        manual edits over Confluence Web UI.
  --space <space>      Use specified space key. If the space key is not specified, it must
                        be set in the page metadata.
  --drop-h1            Don't include H1 headings in Confluence output.
  --title-from-h1      Extract page title from a leading H1 heading. If no H1 heading
                        on a page exists, then title must be set in the page metadata.
  --dry-run            Resolve page and ancestry, show resulting HTML and exit.
  --compile-only       Show resulting HTML and don't update Confluence page content.
  --minor-edit         Don't send notifications while updating Confluence page.
  --debug              Enable debug logs.
  --trace              Enable trace logs.
  --color <when>       Display logs in color. Possible values: auto, never.
                        [default: auto]
  -c --config <path>   Use the specified configuration file.
                        [default: $HOME/.config/mark]
  --ci                 Runs on CI mode. It won't fail if files are not found.
  -h --help            Show this message.
  -v --version         Show version.
`
)

func main() {
	cmd, err := docopt.ParseArgs(os.ExpandEnv(usage), nil, version)
	if err != nil {
		panic(err)
	}

	var flags Flags
	err = cmd.Bind(&flags)
	if err != nil {
		log.Fatal(err)
	}

	if flags.Debug {
		log.SetLevel(lorg.LevelDebug)
	}

	if flags.Trace {
		log.SetLevel(lorg.LevelTrace)
	}

	if flags.Color == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	config, err := LoadConfig(flags.Config)
	if err != nil {
		log.Fatal(err)
	}

	if !flags.TitleFromH1 && config.H1Title {
		flags.TitleFromH1 = true
	}

	if !flags.DropH1 && config.H1Drop {
		flags.DropH1 = true
	}

	creds, err := GetCredentials(flags, config)
	if err != nil {
		log.Fatal(err)
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	files, err := filepath.Glob(flags.FileGlobPatten)
	if err != nil {
		log.Fatal(err)
	}
	if len(files) == 0 {
		msg := "No files matched"
		if flags.Ci {
			log.Warning(msg)
		} else {
			log.Fatal(msg)
		}
	}

	// Loop through files matched by glob pattern
	for _, file := range files {
		log.Infof(
			nil,
			"processing %s",
			file,
		)

		target := processFile(file, api, flags, creds.PageID, creds.Username)

		log.Infof(
			nil,
			"page successfully updated: %s",
			creds.BaseURL+target.Links.Full,
		)

		fmt.Println(creds.BaseURL + target.Links.Full)
	}
}

func processFile(
	file string,
	api *confluence.API,
	flags Flags,
	pageID string,
	username string,
) *confluence.PageInfo {
	markdown, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	meta, markdown, err := mark.ExtractMeta(markdown)
	if err != nil {
		log.Fatal(err)
	}

	if pageID != "" && meta != nil {
		log.Warning(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)

		meta = nil
	}

	if pageID == "" && meta == nil {
		if flags.TitleFromH1 && flags.Space != "" {
			meta = &mark.Meta{}
			meta.Type = "page"
		} else {
			log.Fatal(
				`specified file doesn't contain metadata ` +
					`and URL is not specified via command line ` +
					`or doesn't contain pageId GET-parameter`,
			)
		}
	}

	switch {
	case meta.Space == "" && flags.Space == "":
		log.Fatal(
			"space is not set ('Space' header is not set and '--space' option is not set)",
		)
	case meta.Space == "" && flags.Space != "":
		meta.Space = flags.Space
	}

	if meta.Title == "" && flags.TitleFromH1 {
		meta.Title = mark.ExtractDocumentLeadingH1(markdown)
	}

	if meta.Title == "" {
		log.Fatal(
			`page title is not set ('Title' header is not set ` +
				`and '--title-from-h1' option and 'h1_title' config is not set or there is no H1 in the file)`,
		)
	}

	stdlib, err := stdlib.New(api)
	if err != nil {
		log.Fatal(err)
	}

	templates := stdlib.Templates

	var recurse bool

	for {
		templates, markdown, recurse, err = includes.ProcessIncludes(
			filepath.Dir(file),
			markdown,
			templates,
		)
		if err != nil {
			log.Fatal(err)
		}

		if !recurse {
			break
		}
	}

	macros, markdown, err := macro.ExtractMacros(
		filepath.Dir(file),
		markdown,
		templates,
	)
	if err != nil {
		log.Fatal(err)
	}

	macros = append(macros, stdlib.Macros...)

	for _, macro := range macros {
		markdown, err = macro.Apply(markdown)
		if err != nil {
			log.Fatal(err)
		}
	}

	links, err := mark.ResolveRelativeLinks(api, meta, markdown, ".")
	if err != nil {
		log.Fatalf(err, "unable to resolve relative links")
	}

	markdown = mark.SubstituteLinks(markdown, links)

	if flags.DryRun {
		flags.CompileOnly = true

		_, _, err := mark.ResolvePage(flags.DryRun, api, meta)
		if err != nil {
			log.Fatalf(err, "unable to resolve page location")
		}
	}

	if flags.CompileOnly {
		if flags.DropH1 {
			log.Info(
				"the leading H1 heading will be excluded from the Confluence output",
			)
			markdown = mark.DropDocumentLeadingH1(markdown)
		}

		fmt.Println(mark.CompileMarkdown(markdown, stdlib))
		os.Exit(0)
	}

	var target *confluence.PageInfo

	if meta != nil {
		parent, page, err := mark.ResolvePage(flags.DryRun, api, meta)
		if err != nil {
			log.Fatalf(
				karma.Describe("title", meta.Title).Reason(err),
				"unable to resolve %s",
				meta.Type,
			)
		}

		if page == nil {
			page, err = api.CreatePage(
				meta.Space,
				meta.Type,
				parent,
				meta.Title,
				``,
			)
			if err != nil {
				log.Fatalf(
					err,
					"can't create %s %q",
					meta.Type,
					meta.Title,
				)
			}
			// (issues/139): A delay between the create and update call
			// helps mitigate a 409 conflict that can occur when attempting
			// to update a page just after it was created.
			time.Sleep(1 * time.Second)
		}

		target = page
	} else {
		if pageID == "" {
			log.Fatalf(nil, "URL should provide 'pageId' GET-parameter")
		}

		page, err := api.GetPageByID(pageID)
		if err != nil {
			log.Fatalf(err, "unable to retrieve page by id")
		}

		target = page
	}

	attaches, err := mark.ResolveAttachments(
		api,
		target,
		filepath.Dir(file),
		meta.Attachments,
	)
	if err != nil {
		log.Fatalf(err, "unable to create/update attachments")
	}

	markdown = mark.CompileAttachmentLinks(markdown, attaches)

	if flags.DropH1 {
		log.Info(
			"the leading H1 heading will be excluded from the Confluence output",
		)
		markdown = mark.DropDocumentLeadingH1(markdown)
	}

	html := mark.CompileMarkdown(markdown, stdlib)

	{
		var buffer bytes.Buffer

		err := stdlib.Templates.ExecuteTemplate(
			&buffer,
			"ac:layout",
			struct {
				Layout  string
				Sidebar string
				Body    string
			}{
				Layout:  meta.Layout,
				Sidebar: meta.Sidebar,
				Body:    html,
			},
		)
		if err != nil {
			log.Fatal(err)
		}

		html = buffer.String()
	}

	err = api.UpdatePage(target, html, flags.MinorEdit, meta.Labels)
	if err != nil {
		log.Fatal(err)
	}

	if flags.EditLock {
		log.Infof(
			nil,
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			username,
		)

		err := api.RestrictPageUpdates(target, username)
		if err != nil {
			log.Fatal(err)
		}
	}

	return target
}
