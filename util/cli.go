package util

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/attachment"
	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/includes"
	"github.com/kovetskiy/mark/macro"
	mark "github.com/kovetskiy/mark/markdown"
	"github.com/kovetskiy/mark/metadata"
	"github.com/kovetskiy/mark/page"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/types"
	"github.com/kovetskiy/mark/vfs"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v3"
)

func RunMark(ctx context.Context, cmd *cli.Command) error {
	if err := SetLogLevel(cmd); err != nil {
		return err
	}

	if cmd.String("color") == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	creds, err := GetCredentials(cmd.String("username"), cmd.String("password"), cmd.String("target-url"), cmd.String("base-url"), cmd.Bool("compile-only"))
	if err != nil {
		return err
	}

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password)

	files, err := doublestar.FilepathGlob(cmd.String("files"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		msg := "No files matched"
		if cmd.Bool("ci") {
			log.Warning(msg)
		} else {
			log.Fatal(msg)
		}
	}

	log.Debug("config:")
	for _, f := range cmd.Flags {
		flag := f.Names()
		if flag[0] == "password" {
			log.Debugf(nil, "%20s: %v", flag[0], "******")
		} else {
			log.Debugf(nil, "%20s: %v", flag[0], cmd.Value(flag[0]))
		}
	}

	fatalErrorHandler := NewErrorHandler(cmd.Bool("continue-on-error"))

	// Loop through files matched by glob pattern
	for _, file := range files {
		log.Infof(
			nil,
			"processing %s",
			file,
		)

		target := processFile(file, api, cmd, creds.PageID, creds.Username, fatalErrorHandler)

		if target != nil { // on dry-run or compile-only, the target is nil
			log.Infof(
				nil,
				"page successfully updated: %s",
				creds.BaseURL+target.Links.Full,
			)
			fmt.Println(creds.BaseURL + target.Links.Full)
		}
	}
	return nil
}

func processFile(
	file string,
	api *confluence.API,
	cmd *cli.Command,
	pageID string,
	username string,
	fatalErrorHandler *FatalErrorHandler,
) *confluence.PageInfo {
	markdown, err := os.ReadFile(file)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to read file %q", file)
		return nil
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	parents := strings.Split(cmd.String("parents"), cmd.String("parents-delimiter"))

	meta, markdown, err := metadata.ExtractMeta(markdown, cmd.String("space"), cmd.Bool("title-from-h1"), parents, cmd.Bool("title-append-generated-hash"))
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to extract metadata from file %q", file)
		return nil
	}

	if pageID != "" && meta != nil {
		log.Warning(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)

		meta = nil
	}

	if pageID == "" && meta == nil {
		fatalErrorHandler.Handle(nil, "specified file doesn't contain metadata and URL is not specified via command line or doesn't contain pageId GET-parameter")
		return nil
	}

	if meta != nil {
		if meta.Space == "" {
			fatalErrorHandler.Handle(nil, "space is not set ('Space' header is not set and '--space' option is not set)")
			return nil
		}

		if meta.Title == "" {
			fatalErrorHandler.Handle(nil, "page title is not set ('Title' header is not set and '--title-from-h1' option and 'h1-title' config is not set or there is no H1 in the file)")
			return nil
		}
	}

	stdlib, err := stdlib.New(api)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to retrieve standard library")
		return nil
	}

	templates := stdlib.Templates

	var recurse bool

	for {
		templates, markdown, recurse, err = includes.ProcessIncludes(
			filepath.Dir(file),
			cmd.String("include-path"),
			markdown,
			templates,
		)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to process includes")
			return nil
		}

		if !recurse {
			break
		}
	}

	macros, markdown, err := macro.ExtractMacros(
		filepath.Dir(file),
		cmd.String("include-path"),
		markdown,
		templates,
	)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to extract macros")
		return nil
	}

	macros = append(macros, stdlib.Macros...)

	for _, macro := range macros {
		markdown, err = macro.Apply(markdown)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to apply macro")
			return nil
		}
	}

	links, err := page.ResolveRelativeLinks(api, meta, markdown, filepath.Dir(file), cmd.String("space"), cmd.Bool("title-from-h1"), parents, cmd.Bool("title-append-generated-hash"))
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to resolve relative links")
		return nil
	}

	markdown = page.SubstituteLinks(markdown, links)

	if cmd.Bool("dry-run") {
		_, _, err := page.ResolvePage(cmd.Bool("dry-run"), api, meta)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to resolve page location")
			return nil
		}
	}

	if cmd.Bool("compile-only") || cmd.Bool("dry-run") {
		if cmd.Bool("drop-h1") {
			log.Info(
				"the leading H1 heading will be excluded from the Confluence output",
			)
		}

		cfg := types.MarkConfig{
			MermaidProvider: cmd.String("mermaid-provider"),
			MermaidScale:    cmd.Float("mermaid-scale"),
			D2Scale:         cmd.Float("d2-scale"),
			DropFirstH1:     cmd.Bool("drop-h1"),
			StripNewlines:   cmd.Bool("strip-linebreaks"),
			Features:        cmd.StringSlice("features"),
		}
		html, _ := mark.CompileMarkdown(markdown, stdlib, file, cfg)
		fmt.Println(html)
		return nil
	}

	var target *confluence.PageInfo

	if meta != nil {
		parent, page, err := page.ResolvePage(cmd.Bool("dry-run"), api, meta)
		if err != nil {
			fatalErrorHandler.Handle(karma.Describe("title", meta.Title).Reason(err), "unable to resolve %s", meta.Type)
			return nil
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
				fatalErrorHandler.Handle(err, "can't create %s %q", meta.Type, meta.Title)
				return nil
			}
			// (issues/139): A delay between the create and update call
			// helps mitigate a 409 conflict that can occur when attempting
			// to update a page just after it was created.
			time.Sleep(1 * time.Second)
		}

		target = page
	} else {
		if pageID == "" {
			fatalErrorHandler.Handle(nil, "URL should provide 'pageId' GET-parameter")
			return nil
		}

		page, err := api.GetPageByID(pageID)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to retrieve page by id")
			return nil
		}

		target = page
	}

	// Resolve attachments created from <!-- Attachment: --> directive
	localAttachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(file), meta.Attachments)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to locate attachments")
		return nil
	}

	attaches, err := attachment.ResolveAttachments(
		api,
		target,
		localAttachments,
	)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to create/update attachments")
		return nil
	}

	markdown = attachment.CompileAttachmentLinks(markdown, attaches)

	if cmd.Bool("drop-h1") {
		log.Info(
			"the leading H1 heading will be excluded from the Confluence output",
		)
	}
	cfg := types.MarkConfig{
		MermaidProvider: cmd.String("mermaid-provider"),
		MermaidScale:    cmd.Float("mermaid-scale"),
		D2Scale:         cmd.Float("d2-scale"),
		DropFirstH1:     cmd.Bool("drop-h1"),
		StripNewlines:   cmd.Bool("strip-linebreaks"),
		Features:        cmd.StringSlice("features"),
	}

	html, inlineAttachments := mark.CompileMarkdown(markdown, stdlib, file, cfg)

	// Resolve attachements detected from markdown
	_, err = attachment.ResolveAttachments(
		api,
		target,
		inlineAttachments,
	)
	if err != nil {
		fatalErrorHandler.Handle(err, "unable to create/update attachments")
		return nil
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
			fatalErrorHandler.Handle(err, "unable to execute layout template")
			return nil
		}

		html = buffer.String()
	}

	var finalVersionMessage string
	var shouldUpdatePage = true

	if cmd.Bool("changes-only") {
		contentHash := getSHA1Hash(html)

		log.Debugf(
			nil,
			"content hash: %s",
			contentHash,
		)

		versionPattern := `\[v([a-f0-9]{40})]$`
		re := regexp.MustCompile(versionPattern)

		matches := re.FindStringSubmatch(target.Version.Message)

		if len(matches) > 1 {
			log.Debugf(
				nil,
				"previous content hash: %s",
				matches[1],
			)

			if matches[1] == contentHash {
				log.Infof(
					nil,
					"page %q is already up to date",
					target.Title,
				)
				shouldUpdatePage = false
			}
		}

		finalVersionMessage = fmt.Sprintf("%s [v%s]", cmd.String("version-message"), contentHash)
	} else {
		finalVersionMessage = cmd.String("version-message")
	}

	if shouldUpdatePage {
		err = api.UpdatePage(target, html, cmd.Bool("minor-edit"), finalVersionMessage, meta.Labels, meta.ContentAppearance, meta.Emoji)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to update page")
			return nil
		}
	}

	if !updateLabels(api, target, meta, fatalErrorHandler) { // on error updating labels, return nil
		return nil
	}

	if cmd.Bool("edit-lock") {
		log.Infof(
			nil,
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			username,
		)

		err := api.RestrictPageUpdates(target, username)
		if err != nil {
			fatalErrorHandler.Handle(err, "unable to restrict page updates")
			return nil
		}
	}

	return target
}

func updateLabels(api *confluence.API, target *confluence.PageInfo, meta *metadata.Meta, fatalErrorHandler *FatalErrorHandler) bool {
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
			fatalErrorHandler.Handle(err, "error adding labels")
			return false
		}
	}

	for _, label := range delLabels {
		_, err = api.DeletePageLabel(target, label)
		if err != nil {
			fatalErrorHandler.Handle(err, "error deleting labels")
			return false
		}
	}
	return true
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

func ConfigFilePath() string {
	fp, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(fp, "mark.toml")
}

func SetLogLevel(cmd *cli.Command) error {
	logLevel := cmd.String("log-level")
	switch strings.ToUpper(logLevel) {
	case lorg.LevelTrace.String():
		log.SetLevel(lorg.LevelTrace)
	case lorg.LevelDebug.String():
		log.SetLevel(lorg.LevelDebug)
	case lorg.LevelInfo.String():
		log.SetLevel(lorg.LevelInfo)
	case lorg.LevelWarning.String():
		log.SetLevel(lorg.LevelWarning)
	case lorg.LevelError.String():
		log.SetLevel(lorg.LevelError)
	case lorg.LevelFatal.String():
		log.SetLevel(lorg.LevelFatal)
	default:
		return fmt.Errorf("unknown log level: %s", logLevel)
	}
	log.GetLevel()

	return nil
}

func getSHA1Hash(input string) string {
	hash := sha1.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}
