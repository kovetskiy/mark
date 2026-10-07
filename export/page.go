package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/rs/zerolog/log"
)

// Config says which page Page exports and where to.
type Config struct {
	// PageID names the page. Without it, Space and Title do.
	PageID string
	Space  string
	Title  string

	// Output is the file the Markdown is written to. Empty, or "-", writes
	// it to Stdout.
	Output string

	// AttachmentsDir is where the attachments the page shows or links to are
	// written. Empty is the directory Output is in, or the working directory
	// when the Markdown goes to Stdout.
	AttachmentsDir string

	// Overwrite replaces files that are already there. Without it, finding
	// one fails the export before anything is written.
	Overwrite bool

	// NoAttachments writes the Markdown only, referring to the attachments
	// where they would have been written.
	NoAttachments bool

	// Stdout is where the Markdown goes when Output names no file.
	Stdout io.Writer
}

// Result is what Page wrote.
type Result struct {
	Page *confluence.PageInfo

	// Output is the file the Markdown was written to, or "" for Stdout.
	Output string

	// Attachments are the files the attachments were written to.
	Attachments []string
}

// expansions are what an exported page is read with.
const expansions = "body.storage,ancestors,version,space"

// Page exports a Confluence page to a Markdown file that mark can publish
// back: its body converted to Markdown, below the metadata headers that name
// the page -- its space, its ancestry, its title, its labels -- and with the
// attachments it refers to written beside it.
func Page(ctx context.Context, api *confluence.API, cfg Config) (*Result, error) {
	page, err := findPage(api, cfg)
	if err != nil {
		return nil, err
	}

	space := page.Space.Key
	if space == "" {
		space = cfg.Space
	}
	if space == "" {
		return nil, fmt.Errorf("unable to tell which space page %s is in", page.ID)
	}

	output := cfg.Output
	if output == "-" {
		output = ""
	}

	outputDir := "."
	if output != "" {
		outputDir = filepath.Dir(output)
	}

	attachmentsDir := cfg.AttachmentsDir
	if attachmentsDir == "" {
		attachmentsDir = outputDir
	}

	relative, err := relativePath(outputDir, attachmentsDir)
	if err != nil {
		return nil, fmt.Errorf("unable to refer to %s from %s: %w", attachmentsDir, outputDir, err)
	}

	doc, err := Convert(page.Body.Storage.Value, Options{
		Space: space,
		AttachmentPath: func(filename string) string {
			return filepath.ToSlash(filepath.Join(relative, localName(filename)))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to convert page %s: %w", page.ID, err)
	}

	header, err := headers(api, page, space, doc)
	if err != nil {
		return nil, err
	}

	markdown := header
	if doc.Markdown != "" {
		markdown += "\n" + doc.Markdown
	}

	downloads, err := attachmentsToWrite(api, page, doc, attachmentsDir, cfg.NoAttachments)
	if err != nil {
		return nil, err
	}

	if !cfg.Overwrite {
		targets := make([]string, 0, len(downloads)+1)
		if output != "" {
			targets = append(targets, output)
		}
		for _, d := range downloads {
			targets = append(targets, d.path)
		}
		if err := refuseExisting(targets); err != nil {
			return nil, err
		}
	}

	result := &Result{Page: page, Output: output}

	for _, d := range downloads {
		if err := download(ctx, api, d); err != nil {
			return nil, err
		}
		log.Info().Msgf("attachment %q written to %s", d.info.Filename, d.path)
		result.Attachments = append(result.Attachments, d.path)
	}

	if output == "" {
		if _, err := io.WriteString(cfg.Stdout, markdown); err != nil {
			return nil, fmt.Errorf("unable to write the page: %w", err)
		}
		return result, nil
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("unable to create %s: %w", outputDir, err)
	}
	if err := os.WriteFile(output, []byte(markdown), 0o644); err != nil { //nolint:gosec // G306: a document, meant to be read like the others beside it
		return nil, fmt.Errorf("unable to write %s: %w", output, err)
	}

	log.Info().Msgf("page %q exported to %s", page.Title, output)

	return result, nil
}

// findPage reads the page the configuration names, with everything an export
// needs of it.
func findPage(api *confluence.API, cfg Config) (*confluence.PageInfo, error) {
	id := cfg.PageID

	if id == "" {
		if cfg.Space == "" || cfg.Title == "" {
			return nil, errors.New("a page is named by its id, or by its space and title")
		}

		var found *confluence.PageInfo
		for _, pageType := range []string{"page", "blogpost"} {
			var err error
			found, err = api.FindPage(cfg.Space, cfg.Title, pageType)
			if err != nil {
				return nil, fmt.Errorf("unable to find page %q in space %s: %w", cfg.Title, cfg.Space, err)
			}
			if found != nil {
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("no page or blog post titled %q in space %s", cfg.Title, cfg.Space)
		}

		id = found.ID
	}

	page, err := api.GetPageByIDExpanded(id, expansions)
	if err != nil {
		return nil, fmt.Errorf("unable to read page %s: %w", id, err)
	}

	return page, nil
}

// headers writes the metadata headers that publish the document back to the
// page it came from.
func headers(api *confluence.API, page *confluence.PageInfo, space string, doc *Document) (string, error) {
	var lines []string
	add := func(name, value string) error {
		value, err := headerValue(name, value)
		if err != nil {
			return err
		}
		lines = append(lines, "<!-- "+name+": "+value+" -->")
		return nil
	}

	if err := add(metadata.HeaderSpace, space); err != nil {
		return "", err
	}

	if page.Type == "blogpost" {
		if err := add(metadata.HeaderType, "blogpost"); err != nil {
			return "", err
		}
	} else {
		parents, err := parents(api, page, space)
		if err != nil {
			return "", err
		}
		for _, parent := range parents {
			if err := add(metadata.HeaderParent, parent); err != nil {
				return "", err
			}
		}
	}

	if err := add(metadata.HeaderTitle, page.Title); err != nil {
		return "", err
	}

	labels, err := api.GetPageLabels(page, "global")
	if err != nil {
		return "", fmt.Errorf("unable to read the labels of page %s: %w", page.ID, err)
	}
	// In a stable order, so that exporting a page twice writes the same file.
	names := make([]string, 0, len(labels.Labels))
	for _, label := range labels.Labels {
		names = append(names, label.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := add(metadata.HeaderLabel, name); err != nil {
			return "", err
		}
	}

	switch {
	case doc.Sidebar != "":
		if err := add(metadata.HeaderSidebar, doc.Sidebar); err != nil {
			return "", err
		}
	case doc.Layout != "":
		if err := add(metadata.HeaderLayout, doc.Layout); err != nil {
			return "", err
		}
	}

	appearance, emoji := appearanceOf(api, page)
	if appearance != "" {
		if err := add(metadata.ContentAppearance, appearance); err != nil {
			return "", err
		}
	}
	if emoji != "" {
		if err := add(metadata.HeaderEmoji, emoji); err != nil {
			return "", err
		}
	}

	for _, name := range doc.Declared {
		if err := add(metadata.HeaderAttachment, name); err != nil {
			return "", err
		}
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// parents is the page's ancestry below the space's home page, which is where
// mark starts from when it resolves Parent headers.
func parents(api *confluence.API, page *confluence.PageInfo, space string) ([]string, error) {
	if len(page.Ancestors) == 0 {
		return nil, nil
	}

	home, err := api.FindHomePage(space)
	if err != nil {
		return nil, fmt.Errorf("unable to find the home page of space %s: %w", space, err)
	}

	ancestors := page.Ancestors
	if home != nil && ancestors[0].ID == home.ID {
		ancestors = ancestors[1:]
	} else {
		log.Warn().Msgf(
			"page %q is not below the home page of space %s; publishing it back puts it there",
			page.Title, space,
		)
	}

	titles := make([]string, 0, len(ancestors))
	for _, ancestor := range ancestors {
		titles = append(titles, ancestor.Title)
	}

	return titles, nil
}

// appearanceOf reads the content appearance and the title emoji of a page,
// which Confluence keeps as content properties. Neither is worth failing the
// export over: a token that may not read properties exports the page without
// them, and says so.
func appearanceOf(api *confluence.API, page *confluence.PageInfo) (appearance, emoji string) {
	properties, err := api.ContentProperties(page.ID)
	if err != nil {
		log.Warn().Msgf("unable to read the content appearance and emoji of page %s: %s", page.ID, err)
		return "", ""
	}

	appearance = metadata.DefaultContentAppearance
	for _, property := range properties {
		var value string
		if err := json.Unmarshal(property.Value, &value); err != nil {
			continue
		}

		switch property.Key {
		case "content-appearance-published":
			appearance = value
		case "emoji-title-published":
			emoji = emojiFromHex(value)
		}
	}

	// mark publishes a page full width unless told otherwise, so that is the
	// one appearance there is no need to say.
	switch appearance {
	case metadata.FullWidthContentAppearance:
		appearance = ""
	case metadata.FixedContentAppearance, metadata.DefaultContentAppearance:
	default:
		appearance = ""
	}

	return appearance, emoji
}

// emojiFromHex reads an emoji Confluence stores as its code points in hex,
// separated by dashes when there are several.
func emojiFromHex(value string) string {
	var b strings.Builder
	for _, part := range strings.Split(value, "-") {
		code, err := strconv.ParseInt(part, 16, 32)
		if err != nil || code <= 0 {
			return ""
		}
		b.WriteRune(rune(code))
	}

	return b.String()
}

// relativePath is target as seen from base, whether either is absolute or
// relative to the working directory.
func relativePath(base, target string) (string, error) {
	absoluteBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	return filepath.Rel(absoluteBase, absoluteTarget)
}

// localName is the name an attachment is written under: its own, unless that
// would reach into another directory.
func localName(filename string) string {
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(filename)
	if name == "." || name == ".." {
		name = "_" + name
	}

	return name
}

type pendingDownload struct {
	info confluence.AttachmentInfo
	path string
}

// attachmentsToWrite pairs each attachment the body refers to with the file
// it goes to.
func attachmentsToWrite(
	api *confluence.API,
	page *confluence.PageInfo,
	doc *Document,
	dir string,
	skip bool,
) ([]pendingDownload, error) {
	if skip || len(doc.Attachments) == 0 {
		return nil, nil
	}

	remote, err := api.GetAttachments(page.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the attachments of page %s: %w", page.ID, err)
	}

	byName := make(map[string]confluence.AttachmentInfo, len(remote))
	for _, info := range remote {
		byName[info.Filename] = info
	}

	var downloads []pendingDownload
	for _, name := range doc.Attachments {
		info, ok := byName[name]
		if !ok {
			log.Warn().Msgf("page %q refers to an attachment %q it does not have", page.Title, name)
			continue
		}
		downloads = append(downloads, pendingDownload{info: info, path: filepath.Join(dir, localName(name))})
	}

	return downloads, nil
}

// refuseExisting fails when any of the files an export would write is there
// already, naming every one of them, so that nothing is half written.
func refuseExisting(paths []string) error {
	var existing []string
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			existing = append(existing, path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("unable to check %s: %w", path, err)
		}
	}

	if len(existing) > 0 {
		return fmt.Errorf("refusing to overwrite %s; pass --overwrite to replace them", strings.Join(existing, ", "))
	}

	return nil
}

// download writes an attachment to its file, through a temporary file beside
// it so that a failed download leaves nothing behind.
func download(ctx context.Context, api *confluence.API, d pendingDownload) error {
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("unable to create %s: %w", dir, err)
	}

	temporary, err := os.CreateTemp(dir, ".mark-export-*")
	if err != nil {
		return fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, err)
	}
	defer func() {
		_ = os.Remove(temporary.Name())
	}()

	if err := api.DownloadAttachment(ctx, d.info, temporary); err != nil {
		_ = temporary.Close()
		return err
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, err)
	}

	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, err)
	}

	if err := os.Rename(temporary.Name(), d.path); err != nil {
		return fmt.Errorf("unable to write attachment %q to %s: %w", d.info.Filename, d.path, err)
	}

	return nil
}
