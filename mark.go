package mark

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/includes"
	"github.com/kovetskiy/mark/v16/macro"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/rs/zerolog/log"
)

var markerRegex = regexp.MustCompile(`<ac:inline-comment-marker ac:ref="([^"]+)">([^<]*)</ac:inline-comment-marker>`)

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
	PreserveComments bool

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
			log.Warn().Msg(msg)
		} else {
			return errors.New(msg)
		}
	}

	var hasErrors bool
	for _, file := range files {
		log.Info().Msgf("processing %s", file)

		target, err := ProcessFile(file, api, config)
		if err != nil {
			if config.ContinueOnError {
				log.Error().Err(err).Msgf("processing %s", file)
				hasErrors = true
				continue
			}
			return err
		}

		if target != nil {
			log.Info().Msgf("page successfully updated: %s", api.BaseURL+target.Links.Full)
			if _, err := fmt.Fprintln(config.output(), api.BaseURL+target.Links.Full); err != nil {
				return err
			}
		}
	}

	if hasErrors {
		return fmt.Errorf("one or more files failed to process")
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
		log.Warn().Msg(
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
			if _, err := api.GetPageByID(config.PageID, ""); err != nil {
				return nil, fmt.Errorf("unable to resolve page by ID: %w", err)
			}
		}
	}

	if config.CompileOnly || config.DryRun {
		if config.DropH1 {
			log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
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
		html, _, err := markmd.CompileMarkdown(markdown, std, file, cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to compile markdown: %w", err)
		}
		if _, err := fmt.Fprintln(config.output(), html); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var target *confluence.PageInfo
	var pageCreated bool

	if meta != nil {
		parent, pg, err := page.ResolvePage(false, api, meta)
		if err != nil {
			return nil, fmt.Errorf("error resolving page %q: %w", meta.Title, err)
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
			pageCreated = true
		}

		target = pg
	} else {
		pg, err := api.GetPageByID(config.PageID, "")
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		if pg == nil {
			return nil, fmt.Errorf("page with id %q not found", config.PageID)
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
		log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
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

	html, inlineAttachments, err := markmd.CompileMarkdown(markdown, std, file, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to compile markdown: %w", err)
	}

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

	if config.PreserveComments && !pageCreated {
		pg, err := api.GetPageByID(target.ID, "ancestors,version,body.storage")
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page body for comments: %w", err)
		}
		target = pg

		comments, err := api.GetInlineComments(target.ID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve inline comments: %w", err)
		}

		html, err = MergeComments(html, target.Body.Storage.Value, comments)
		if err != nil {
			return nil, fmt.Errorf("unable to merge inline comments: %w", err)
		}
	}

	var finalVersionMessage string
	shouldUpdatePage := true

	if config.ChangesOnly {
		contentHash := sha1Hash(html)
		log.Debug().Msgf("content hash: %s", contentHash)

		re := regexp.MustCompile(`\[v([a-f0-9]{40})]$`)
		if matches := re.FindStringSubmatch(target.Version.Message); len(matches) > 1 {
			log.Debug().Msgf("previous content hash: %s", matches[1])
			if matches[1] == contentHash {
				log.Info().Msgf("page %q is already up to date", target.Title)
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
		log.Info().Msgf(
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

	log.Debug().Msg("Page Labels:")
	log.Debug().Interface("labels", labelInfo.Labels).Send()
	log.Debug().Msg("Meta Labels:")
	log.Debug().Interface("labels", metaLabels).Send()

	delLabels := determineLabelsToRemove(labelInfo, metaLabels)
	log.Debug().Msg("Del Labels:")
	log.Debug().Interface("labels", delLabels).Send()

	addLabels := determineLabelsToAdd(metaLabels, labelInfo)
	log.Debug().Msg("Add Labels:")
	log.Debug().Interface("labels", addLabels).Send()

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

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	r1 := []rune(s1)
	r2 := []rune(s2)

	rows := len(r1) + 1
	cols := len(r2) + 1

	dist := make([][]int, rows)
	for i := range dist {
		dist[i] = make([]int, cols)
	}

	for i := 0; i < rows; i++ {
		dist[i][0] = i
	}
	for j := 0; j < cols; j++ {
		dist[0][j] = j
	}

	for i := 1; i < rows; i++ {
		for j := 1; j < cols; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			dist[i][j] = min(
				dist[i-1][j]+1,      // deletion
				dist[i][j-1]+1,      // insertion
				dist[i-1][j-1]+cost, // substitution
			)
		}
	}
	return dist[len(r1)][len(r2)]
}

func min(vals ...int) int {
	res := vals[0]
	for _, v := range vals[1:] {
		if v < res {
			res = v
		}
	}
	return res
}

type commentContext struct {
	before string
	after  string
}

func MergeComments(newBody string, oldBody string, comments *confluence.InlineComments) (string, error) {
	if comments == nil {
		return newBody, nil
	}
	// 1. Extract context for each comment from oldBody
	contexts := make(map[string]commentContext)
	matches := markerRegex.FindAllStringSubmatchIndex(oldBody, -1)
	for _, match := range matches {
		ref := oldBody[match[2]:match[3]]
		// context around the tag
		before := oldBody[max(0, match[0]-100):match[0]]
		after := oldBody[match[1]:min(len(oldBody), match[1]+100)]
		contexts[ref] = commentContext{
			before: before,
			after:  after,
		}
	}

	type replacement struct {
		start int
		end   int
		ref   string
	}
	var replacements []replacement

	for _, comment := range comments.Results {
		if comment.Extensions.Location != "inline" {
			continue
		}

		ref := comment.Extensions.InlineProperties.MarkerRef
		selection := comment.Extensions.InlineProperties.OriginalSelection

		ctx, hasCtx := contexts[ref]

		// Find all occurrences of selection in newBody
		// Selection in newBody might be HTML escaped
		escapedSelection := html.EscapeString(selection)

		var bestStart = -1
		var bestEnd = -1
		var minDistance = 1000000

		// Find all occurrences
		currentPos := 0
		for {
			index := strings.Index(newBody[currentPos:], escapedSelection)
			if index == -1 {
				break
			}
			start := currentPos + index
			end := start + len(escapedSelection)

			if !hasCtx {
				// No context available; use the first occurrence.
				bestStart = start
				bestEnd = end
				break
			}

			newBefore := newBody[max(0, start-100):start]
			newAfter := newBody[end:min(len(newBody), end+100)]
			distance := levenshteinDistance(ctx.before, newBefore) + levenshteinDistance(ctx.after, newAfter)

			if distance < minDistance {
				minDistance = distance
				bestStart = start
				bestEnd = end
			}

			currentPos = start + 1
		}

		if bestStart != -1 {
			replacements = append(replacements, replacement{
				start: bestStart,
				end:   bestEnd,
				ref:   ref,
			})
		}
	}

	// Sort replacements from back to front to avoid offset issues
	slices.SortFunc(replacements, func(a, b replacement) int {
		return b.start - a.start
	})

	// Apply replacements back-to-front. Track the minimum start of any
	// applied replacement so that overlapping candidates (whose end exceeds
	// that boundary) are dropped rather than producing nested or malformed
	// <ac:inline-comment-marker> tags.
	minAppliedStart := len(newBody)
	for _, r := range replacements {
		if r.end > minAppliedStart {
			// This replacement overlaps with an already-applied one.
			// Drop it and warn so the user knows the comment was skipped.
			log.Warn().
				Str("ref", r.ref).
				Int("start", r.start).
				Int("end", r.end).
				Int("conflicting_start", minAppliedStart).
				Msg("inline comment marker dropped: selection overlaps an already-placed marker")
			continue
		}
		minAppliedStart = r.start
		selection := newBody[r.start:r.end]
		withComment := fmt.Sprintf(
			`<ac:inline-comment-marker ac:ref="%s">%s</ac:inline-comment-marker>`,
			html.EscapeString(r.ref),
			selection,
		)
		newBody = newBody[:r.start] + withComment + newBody[r.end:]
	}

	return newBody, nil
}
