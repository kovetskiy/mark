package mark

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/attachment"
	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/includes"
	"github.com/kovetskiy/mark/macro"
	markmd "github.com/kovetskiy/mark/markdown"
	"github.com/kovetskiy/mark/metadata"
	"github.com/kovetskiy/mark/page"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/types"
	"github.com/kovetskiy/mark/vfs"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

// Config holds all configuration options for running Mark.
type Config struct {
	// Connection settings
	BaseURL               string
	Username              string
	Password              string
	PageID                string
	InsecureSkipTLSVerify bool

	// File selection
	Files string

	// Behaviour
	CompileOnly     bool
	DryRun          bool
	ContinueOnError bool
	CI              bool

	// Page content
	Space                    string
	Parents                  []string
	TitleFromH1              bool
	TitleFromFilename        bool
	TitleAppendGeneratedHash bool
	ContentAppearance        string

	// Page updates
	MinorEdit      bool
	VersionMessage string
	EditLock       bool
	ChangesOnly    bool

	// Rendering
	DropH1          bool
	StripLinebreaks bool
	MermaidScale    float64
	D2Scale         float64
	Features        []string
	ImageAlign      string
	IncludePath     string

	// Output is the writer used for result output (e.g. published page URLs,
	// compiled HTML). If nil, output is discarded; the CLI sets this to
	// os.Stdout.
	Output io.Writer
}

// output returns the configured writer, falling back to io.Discard so that
// library callers that do not set Output receive no implicit stdout writes.
func (c Config) output() io.Writer {
	if c.Output != nil {
		return c.Output
	}
	return io.Discard
}

// Run processes all files matching Config.Files and publishes them to Confluence.
func Run(config Config) error {
	api := confluence.NewAPI(config.BaseURL, config.Username, config.Password, config.InsecureSkipTLSVerify)

	files, err := doublestar.FilepathGlob(config.Files)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		msg := "no files matched"
		if config.CI {
			log.Warning(msg)
		} else {
			return fmt.Errorf("%s", msg)
		}
	}

	for _, file := range files {
		log.Infof(nil, "processing %s", file)

		target, err := ProcessFile(file, api, config)
		if err != nil {
			if config.ContinueOnError {
				log.Errorf(err, "processing %s", file)
				continue
			}
			return err
		}

		if target != nil {
			log.Infof(nil, "page successfully updated: %s", config.BaseURL+target.Links.Full)
			if _, err := fmt.Fprintln(config.output(), config.BaseURL+target.Links.Full); err != nil {
				return err
			}
		}
	}

	return nil
}

// ProcessFile processes a single markdown file and publishes it to Confluence.
// Returns nil for the page info when compile-only or dry-run mode is active.
func ProcessFile(file string, api *confluence.API, config Config) (*confluence.PageInfo, error) {
	markdown, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %q: %w", file, err)
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	meta, markdown, err := metadata.ExtractMeta(
		markdown,
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		file,
		config.Parents,
		config.TitleAppendGeneratedHash,
		config.ContentAppearance,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to extract metadata from file %q: %w", file, err)
	}

	if config.PageID != "" && meta != nil {
		log.Warning(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)
		meta = nil
	}

	if config.PageID == "" && meta == nil {
		return nil, fmt.Errorf(
			"specified file doesn't contain metadata and URL is not specified " +
				"via command line or doesn't contain pageId GET-parameter",
		)
	}

	if meta != nil {
		if meta.Space == "" {
			return nil, fmt.Errorf(
				"space is not set ('Space' header is not set and '--space' option is not set)",
			)
		}
		if meta.Title == "" {
			return nil, fmt.Errorf(
				"page title is not set: use the 'Title' header, " +
					"or the --title-from-h1 / --title-from-filename flags",
			)
		}
	}

	std, err := stdlib.New(api)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	templates := std.Templates

	var recurse bool
	for {
		templates, markdown, recurse, err = includes.ProcessIncludes(
			filepath.Dir(file),
			config.IncludePath,
			markdown,
			templates,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to process includes: %w", err)
		}
		if !recurse {
			break
		}
	}

	macros, markdown, err := macro.ExtractMacros(
		filepath.Dir(file),
		config.IncludePath,
		markdown,
		templates,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to extract macros: %w", err)
	}

	for _, m := range macros {
		markdown, err = m.Apply(markdown)
		if err != nil {
			return nil, fmt.Errorf("unable to apply macro: %w", err)
		}
	}

	links, err := page.ResolveRelativeLinks(
		api,
		meta,
		markdown,
		filepath.Dir(file),
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		config.Parents,
		config.TitleAppendGeneratedHash,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve relative links: %w", err)
	}

	markdown = page.SubstituteLinks(markdown, links)

	if config.DryRun {
		if meta != nil {
			if _, _, err := page.ResolvePage(true, api, meta); err != nil {
				return nil, fmt.Errorf("unable to resolve page location: %w", err)
			}
		} else if config.PageID != "" {
			if _, err := api.GetPageByID(config.PageID); err != nil {
				return nil, fmt.Errorf("unable to resolve page by ID: %w", err)
			}
		}
	}

	if config.CompileOnly || config.DryRun {
		if config.DropH1 {
			log.Info("the leading H1 heading will be excluded from the Confluence output")
		}

		imageAlign, err := getImageAlign(config.ImageAlign, meta)
		if err != nil {
			return nil, fmt.Errorf("unable to determine image-align: %w", err)
		}

		cfg := types.MarkConfig{
			MermaidScale:  config.MermaidScale,
			D2Scale:       config.D2Scale,
			DropFirstH1:   config.DropH1,
			StripNewlines: config.StripLinebreaks,
			Features:      config.Features,
			ImageAlign:    imageAlign,
		}
		html, _ := markmd.CompileMarkdown(markdown, std, file, cfg)
		if _, err := fmt.Fprintln(config.output(), html); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var target *confluence.PageInfo

	if meta != nil {
		parent, pg, err := page.ResolvePage(false, api, meta)
		if err != nil {
			return nil, karma.Describe("title", meta.Title).Reason(err)
		}

		if pg == nil {
			pg, err = api.CreatePage(meta.Space, meta.Type, parent, meta.Title, ``)
			if err != nil {
				return nil, fmt.Errorf("can't create %s %q: %w", meta.Type, meta.Title, err)
			}
			// A delay between the create and update call helps mitigate a 409
			// conflict that can occur when attempting to update a page just
			// after it was created. See issues/139.
			time.Sleep(1 * time.Second)
		}

		target = pg
	} else {
		pg, err := api.GetPageByID(config.PageID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		target = pg
	}

	// Collect attachments declared via <!-- Attachment: --> directives.
	var declaredAttachments []string
	if meta != nil {
		declaredAttachments = meta.Attachments
	}

	localAttachments, err := attachment.ResolveLocalAttachments(
		vfs.LocalOS,
		filepath.Dir(file),
		declaredAttachments,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	attaches, err := attachment.ResolveAttachments(api, target, localAttachments)
	if err != nil {
		return nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	markdown = attachment.CompileAttachmentLinks(markdown, attaches)

	if config.DropH1 {
		log.Info("the leading H1 heading will be excluded from the Confluence output")
	}

	imageAlign, err := getImageAlign(config.ImageAlign, meta)
	if err != nil {
		return nil, fmt.Errorf("unable to determine image-align: %w", err)
	}

	cfg := types.MarkConfig{
		MermaidScale:  config.MermaidScale,
		D2Scale:       config.D2Scale,
		DropFirstH1:   config.DropH1,
		StripNewlines: config.StripLinebreaks,
		Features:      config.Features,
		ImageAlign:    imageAlign,
	}

	html, inlineAttachments := markmd.CompileMarkdown(markdown, std, file, cfg)

	if _, err = attachment.ResolveAttachments(api, target, inlineAttachments); err != nil {
		return nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	var layout, sidebar string
	var labels []string
	var contentAppearance, emoji string

	if meta != nil {
		layout = meta.Layout
		sidebar = meta.Sidebar
		labels = meta.Labels
		contentAppearance = meta.ContentAppearance
		emoji = meta.Emoji
	}

	{
		var buffer bytes.Buffer
		err := std.Templates.ExecuteTemplate(
			&buffer,
			"ac:layout",
			struct {
				Layout  string
				Sidebar string
				Body    string
			}{
				Layout:  layout,
				Sidebar: sidebar,
				Body:    html,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("unable to execute layout template: %w", err)
		}
		html = buffer.String()
	}

	var finalVersionMessage string
	shouldUpdatePage := true

	if config.ChangesOnly {
		contentHash := sha1Hash(html)
		log.Debugf(nil, "content hash: %s", contentHash)

		re := regexp.MustCompile(`\[v([a-f0-9]{40})]$`)
		if matches := re.FindStringSubmatch(target.Version.Message); len(matches) > 1 {
			log.Debugf(nil, "previous content hash: %s", matches[1])
			if matches[1] == contentHash {
				log.Infof(nil, "page %q is already up to date", target.Title)
				shouldUpdatePage = false
			}
		}

		finalVersionMessage = fmt.Sprintf("%s [v%s]", config.VersionMessage, contentHash)
	} else {
		finalVersionMessage = config.VersionMessage
	}

	if shouldUpdatePage {
		err = api.UpdatePage(
			target,
			html,
			config.MinorEdit,
			finalVersionMessage,
			labels,
			contentAppearance,
			emoji,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to update page: %w", err)
		}
	}

	if meta != nil {
		if err := updateLabels(api, target, labels); err != nil {
			return nil, err
		}
	}

	if config.EditLock {
		log.Infof(
			nil,
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			config.Username,
		)
		if err := api.RestrictPageUpdates(target, config.Username); err != nil {
			return nil, fmt.Errorf("unable to restrict page updates: %w", err)
		}
	}

	return target, nil
}

func updateLabels(api *confluence.API, target *confluence.PageInfo, metaLabels []string) error {
	labelInfo, err := api.GetPageLabels(target, "global")
	if err != nil {
		return err
	}

	log.Debug("Page Labels:")
	log.Debug(labelInfo.Labels)
	log.Debug("Meta Labels:")
	log.Debug(metaLabels)

	delLabels := determineLabelsToRemove(labelInfo, metaLabels)
	log.Debug("Del Labels:")
	log.Debug(delLabels)

	addLabels := determineLabelsToAdd(metaLabels, labelInfo)
	log.Debug("Add Labels:")
	log.Debug(addLabels)

	if len(addLabels) > 0 {
		if _, err = api.AddPageLabels(target, addLabels); err != nil {
			return fmt.Errorf("error adding labels: %w", err)
		}
	}

	for _, label := range delLabels {
		if _, err = api.DeletePageLabel(target, label); err != nil {
			return fmt.Errorf("error deleting label %q: %w", label, err)
		}
	}

	return nil
}

func determineLabelsToRemove(labelInfo *confluence.LabelInfo, metaLabels []string) []string {
	var labels []string
	for _, label := range labelInfo.Labels {
		if !slices.ContainsFunc(metaLabels, func(metaLabel string) bool {
			return strings.EqualFold(metaLabel, label.Name)
		}) {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

func determineLabelsToAdd(metaLabels []string, labelInfo *confluence.LabelInfo) []string {
	var labels []string
	for _, metaLabel := range metaLabels {
		if !slices.ContainsFunc(labelInfo.Labels, func(label confluence.Label) bool {
			return strings.EqualFold(label.Name, metaLabel)
		}) {
			labels = append(labels, metaLabel)
		}
	}
	return labels
}

func getImageAlign(align string, meta *metadata.Meta) (string, error) {
	if meta != nil && meta.ImageAlign != "" {
		align = meta.ImageAlign
	}

	if align != "" {
		align = strings.ToLower(strings.TrimSpace(align))
		if align != "left" && align != "center" && align != "right" {
			return "", fmt.Errorf(
				`unknown image-align %q, expected one of: left, center, right`,
				align,
			)
		}
		return align, nil
	}

	return "", nil
}

func sha1Hash(input string) string {
	h := sha1.New()
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}
