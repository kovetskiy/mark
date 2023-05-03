package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/mark"
	"github.com/kovetskiy/mark/pkg/mark/includes"
	"github.com/kovetskiy/mark/pkg/mark/macro"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/kovetskiy/mark/pkg/mark/vfs"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const (
	version     = "9.2.1"
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`
)

var flags = []cli.Flag{
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:      "files",
		Aliases:   []string{"f"},
		Value:     "",
		Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
		TakesFile: true,
		EnvVars:   []string{"MARK_FILES"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "compile-only",
		Value:   false,
		Usage:   "show resulting HTML and don't update Confluence page content.",
		EnvVars: []string{"MARK_COMPILE_ONLY"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "dry-run",
		Value:   false,
		Usage:   "resolve page and ancestry, show resulting HTML and exit.",
		EnvVars: []string{"MARK_DRY_RUN"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "edit-lock",
		Value:   false,
		Aliases: []string{"k"},
		Usage:   "lock page editing to current user only to prevent accidental manual edits over Confluence Web UI.",
		EnvVars: []string{"MARK_EDIT_LOCK"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "drop-h1",
		Value:   false,
		Aliases: []string{"h1_drop"},
		Usage:   "don't include H1 headings in Confluence output.",
		EnvVars: []string{"MARK_H1_DROP"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "title-from-h1",
		Value:   false,
		Aliases: []string{"h1_title"},
		Usage:   "extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata.",
		EnvVars: []string{"MARK_H1_TITLE"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "minor-edit",
		Value:   false,
		Usage:   "don't send notifications while updating Confluence page.",
		EnvVars: []string{"MARK_MINOR_EDIT"},
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
		Name:    "target-url",
		Aliases: []string{"l"},
		Value:   "",
		Usage:   "edit specified Confluence page. If -l is not specified, file should contain metadata (see above).",
		EnvVars: []string{"MARK_TARGET_URL"},
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
		Value:     filepath.Join(os.Getenv("HOME"), ".config/mark"),
		Usage:     "use the specified configuration file.",
		TakesFile: true,
		EnvVars:   []string{"MARK_CONFIG"},
	},
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "ci",
		Value:   false,
		Usage:   "run on CI mode. It won't fail if files are not found.",
		EnvVars: []string{"MARK_CI"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "space",
		Value:   "",
		Usage:   "use specified space key. If the space key is not specified, it must be set in the page metadata.",
		EnvVars: []string{"MARK_SPACE"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "mermaid-provider",
		Value:   "cloudscript",
		Usage:   "defines the mermaid provider to use. Supported options are: cloudscript, mermaid-go.",
		EnvVars: []string{"MARK_MERMAID_PROVIDER"},
	}),
}

func main() {
	app := &cli.App{
		Name:        "mark",
		Usage:       usage,
		Description: description,
		Version:     version,
		Flags:       flags,
		Before: altsrc.InitInputSourceWithContext(flags,
			func(context *cli.Context) (altsrc.InputSourceContext, error) {
				if context.IsSet("config") {
					filePath := context.String("config")
					return altsrc.NewTomlSourceFromFile(filePath)
				} else {
					// Fall back to default if config is unset and path exists
					_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config/mark"))
					if os.IsNotExist(err) {
						return &altsrc.MapInputSource{}, nil
					}
					return altsrc.NewTomlSourceFromFile(filepath.Join(os.Getenv("HOME"), ".config/mark"))
				}
			}),
		EnableBashCompletion: true,
		HideHelpCommand:      true,
		Action: func(cCtx *cli.Context) error {
			return RunMark(cCtx)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func RunMark(cCtx *cli.Context) error {

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

	creds, err := GetCredentials(cCtx.String("username"), cCtx.String("password"), cCtx.String("target-url"), cCtx.String("base-url"), cCtx.Bool("compile-only"))
	if err != nil {
		return err
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	files, err := filepath.Glob(cCtx.String("files"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		msg := "No files matched"
		if cCtx.Bool("ci") {
			log.Warning(msg)
		} else {
			log.Fatal(msg)
		}
	}

	log.Debug("config:")
	for _, f := range cCtx.Command.Flags {
		flag := f.Names()
		if flag[0] == "password" {
			log.Debugf(nil, "%20s: %v", flag[0], "******")
		} else {
			log.Debugf(nil, "%20s: %v", flag[0], cCtx.Value(flag[0]))
		}
	}

	// Loop through files matched by glob pattern
	for _, file := range files {
		log.Infof(
			nil,
			"processing %s",
			file,
		)

		target := processFile(file, api, cCtx, creds.PageID, creds.Username)

		log.Infof(
			nil,
			"page successfully updated: %s",
			creds.BaseURL+target.Links.Full,
		)

		fmt.Println(creds.BaseURL + target.Links.Full)
	}
	return nil
}

func processFile(
	file string,
	api *confluence.API,
	cCtx *cli.Context,
	pageID string,
	username string,
) *confluence.PageInfo {
	markdown, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	meta, markdown, err := mark.ExtractMeta(markdown, cCtx.String("space"), cCtx.Bool("title-from-h1"))
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
		log.Fatal(
			`specified file doesn't contain metadata ` +
				`and URL is not specified via command line ` +
				`or doesn't contain pageId GET-parameter`,
		)
	}

	if meta.Space == "" {
		log.Fatal(
			"space is not set ('Space' header is not set and '--space' option is not set)",
		)
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

	links, err := mark.ResolveRelativeLinks(api, meta, markdown, filepath.Dir(file), cCtx.String("space"), cCtx.Bool("title-from-h1"))
	if err != nil {
		log.Fatalf(err, "unable to resolve relative links")
	}

	markdown = mark.SubstituteLinks(markdown, links)

	if cCtx.Bool("dry-run") {
		_, _, err := mark.ResolvePage(cCtx.Bool("dry-run"), api, meta)
		if err != nil {
			log.Fatalf(err, "unable to resolve page location")
		}
	}

	if cCtx.Bool("compile-only") || cCtx.Bool("dry-run") {
		if cCtx.Bool("drop-h1") {
			log.Info(
				"the leading H1 heading will be excluded from the Confluence output",
			)
			markdown = mark.DropDocumentLeadingH1(markdown)
		}

		html, _ := mark.CompileMarkdown(markdown, stdlib, file, cCtx.String("mermaid-provider"))
		fmt.Println(html)
		os.Exit(0)
	}

	var target *confluence.PageInfo

	if meta != nil {
		parent, page, err := mark.ResolvePage(cCtx.Bool("dry-run"), api, meta)
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

	// Resolve attachments created from <!-- Attachment: --> directive
	localAttachments, err := mark.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(file), meta.Attachments)
	if err != nil {
		log.Fatalf(err, "unable to locate attachments")
	}

	attaches, err := mark.ResolveAttachments(
		api,
		target,
		localAttachments,
	)
	if err != nil {
		log.Fatalf(err, "unable to create/update attachments")
	}

	markdown = mark.CompileAttachmentLinks(markdown, attaches)

	if cCtx.Bool("drop-h1") {
		log.Info(
			"the leading H1 heading will be excluded from the Confluence output",
		)
		markdown = mark.DropDocumentLeadingH1(markdown)
	}

	html, inlineAttachments := mark.CompileMarkdown(markdown, stdlib, file, cCtx.String("mermaid-provider"))

	// Resolve attachements detected from markdown
	_, err = mark.ResolveAttachments(
		api,
		target,
		inlineAttachments,
	)
	if err != nil {
		log.Fatalf(err, "unable to create/update attachments")
	}

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

	err = api.UpdatePage(target, html, cCtx.Bool("minor-edit"), meta.Labels, meta.ContentAppearance)
	if err != nil {
		log.Fatal(err)
	}

	if cCtx.Bool("edit-lock") {
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
