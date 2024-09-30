package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/attachment"
	"github.com/kovetskiy/mark/auth"
	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/includes"
	"github.com/kovetskiy/mark/macro"
	mark "github.com/kovetskiy/mark/markdown"
	"github.com/kovetskiy/mark/metadata"
	"github.com/kovetskiy/mark/page"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/vfs"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var PublishCmd = &cli.Command{
	Name:  "publish",
	Usage: "renders markdown and publishes content to confluence.",
	Flags: []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:      "files",
			Aliases:   []string{"f"},
			Value:     "",
			Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
			TakesFile: true,
			EnvVars:   []string{"MARK_FILES"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "space",
			Value:   "",
			Usage:   "use specified space key. If the space key is not specified, it must be set in the page metadata.",
			EnvVars: []string{"MARK_SPACE"},
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
			Usage:   "don't include the first H1 heading in Confluence output.",
			EnvVars: []string{"MARK_H1_DROP"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "strip-linebreaks",
			Value:   false,
			Aliases: []string{"L"},
			Usage:   "remove linebreaks inside of tags, to accomodate non-standard Confluence behavior",
			EnvVars: []string{"MARK_STRIP_LINEBREAK"},
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
			Name:    "version-message",
			Value:   "",
			Usage:   "add a message to the page version, to explain the edit (default: \"\")",
			EnvVars: []string{"MARK_VERSION_MESSAGE"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "parents",
			Value:   "",
			Usage:   "A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be prepended to the ones defined in the document itself.",
			EnvVars: []string{"MARK_PARENTS"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "parents-delimiter",
			Value:   "/",
			Usage:   "The delimiter used for the parents list",
			EnvVars: []string{"MARK_PARENTS_DELIMITER"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "mermaid-provider",
			Value:   "cloudscript",
			Usage:   "defines the mermaid provider to use. Supported options are: cloudscript, mermaid-go.",
			EnvVars: []string{"MARK_MERMAID_PROVIDER"},
		}),
		altsrc.NewFloat64Flag(&cli.Float64Flag{
			Name:    "mermaid-scale",
			Value:   1.0,
			Usage:   "defines the scaling factor for mermaid renderings.",
			EnvVars: []string{"MARK_MERMAID_SCALE"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:      "include-path",
			Value:     "",
			Usage:     "Path for shared includes, used as a fallback if the include doesn't exist in the current directory.",
			TakesFile: true,
			EnvVars:   []string{"MARK_INCLUDE_PATH"},
		}),
	},
	Action: Publish,
}

func Publish(cCtx *cli.Context) error {

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

	creds, err := auth.GetCredentials(cCtx.String("username"), cCtx.String("password"), cCtx.String("target-url"), cCtx.String("base-url"), cCtx.Bool("compile-only"))
	if err != nil {
		return err
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	files, err := doublestar.FilepathGlob(cCtx.String("files"))
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

	parents := strings.Split(cCtx.String("parents"), cCtx.String("parents-delimiter"))

	meta, markdown, err := metadata.ExtractMeta(markdown, cCtx.String("space"), cCtx.Bool("title-from-h1"), parents)
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
			cCtx.String("include-path"),
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
		cCtx.String("include-path"),
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

	links, err := page.ResolveRelativeLinks(api, meta, markdown, filepath.Dir(file), cCtx.String("space"), cCtx.Bool("title-from-h1"), parents)
	if err != nil {
		log.Fatalf(err, "unable to resolve relative links")
	}

	markdown = page.SubstituteLinks(markdown, links)

	if cCtx.Bool("dry-run") {
		_, _, err := page.ResolvePage(cCtx.Bool("dry-run"), api, meta)
		if err != nil {
			log.Fatalf(err, "unable to resolve page location")
		}
	}

	if cCtx.Bool("compile-only") || cCtx.Bool("dry-run") {
		if cCtx.Bool("drop-h1") {
			log.Info(
				"the leading H1 heading will be excluded from the Confluence output",
			)
		}

		html, _ := mark.CompileMarkdown(markdown, stdlib, file, cCtx.String("mermaid-provider"), cCtx.Float64("mermaid-scale"), cCtx.Bool("drop-h1"), cCtx.Bool("strip-linebreaks"))
		fmt.Println(html)
		os.Exit(0)
	}

	var target *confluence.PageInfo

	if meta != nil {
		parent, page, err := page.ResolvePage(cCtx.Bool("dry-run"), api, meta)
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
	localAttachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(file), meta.Attachments)
	if err != nil {
		log.Fatalf(err, "unable to locate attachments")
	}

	attaches, err := attachment.ResolveAttachments(
		api,
		target,
		localAttachments,
	)
	if err != nil {
		log.Fatalf(err, "unable to create/update attachments")
	}

	markdown = attachment.CompileAttachmentLinks(markdown, attaches)

	if cCtx.Bool("drop-h1") {
		log.Info(
			"the leading H1 heading will be excluded from the Confluence output",
		)
	}

	html, inlineAttachments := mark.CompileMarkdown(markdown, stdlib, file, cCtx.String("mermaid-provider"), cCtx.Float64("mermaid-scale"), cCtx.Bool("drop-h1"), cCtx.Bool("strip-linebreaks"))

	// Resolve attachements detected from markdown
	_, err = attachment.ResolveAttachments(
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

	err = api.UpdatePage(target, html, cCtx.Bool("minor-edit"), cCtx.String("version-message"), meta.Labels, meta.ContentAppearance)
	if err != nil {
		log.Fatal(err)
	}

	updateLabels(api, target, meta)

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

func updateLabels(api *confluence.API, target *confluence.PageInfo, meta *metadata.Meta) {

	labelInfo, err := api.GetPageLabels(target, "global")
	if err != nil {
		log.Fatal(err)
	}

	log.Debug("Page Labels:")
	log.Debug(labelInfo.Labels)

	log.Debug("Meta Labels:")
	log.Debug(meta.Labels)

	delLabels := determineLabelsToRemove(labelInfo, meta)
	log.Debug("Del Labels:")
	log.Debug(delLabels)

	addLabels := determineLabelsToAdd(meta, labelInfo)
	log.Debug("Add Labels:")
	log.Debug(addLabels)

	if len(addLabels) > 0 {
		_, err = api.AddPageLabels(target, addLabels)
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, label := range delLabels {
		_, err = api.DeletePageLabel(target, label)
		if err != nil {
			log.Fatal(err)
		}
	}
}

// Page has label but label not in Metadata
func determineLabelsToRemove(labelInfo *confluence.LabelInfo, meta *metadata.Meta) []string {
	var labels []string
	for _, label := range labelInfo.Labels {
		if !slices.ContainsFunc(meta.Labels, func(metaLabel string) bool {
			return strings.EqualFold(metaLabel, label.Name)
		}) {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

// Metadata has label but Page does not have it
func determineLabelsToAdd(meta *metadata.Meta, labelInfo *confluence.LabelInfo) []string {
	var labels []string
	for _, metaLabel := range meta.Labels {
		if !slices.ContainsFunc(labelInfo.Labels, func(label confluence.Label) bool {
			return strings.EqualFold(label.Name, metaLabel)
		}) {
			labels = append(labels, metaLabel)
		}
	}
	return labels
}

func configFilePath() string {
	fp, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(fp, "mark")
}
