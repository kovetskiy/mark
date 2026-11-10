package export

import (
	"cmp"
	"context"
	"crypto/sha256"
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
	"unicode/utf8"

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

	// Format is FormatMark, or "", for a document mark publishes back, or
	// FormatOKF for an Open Knowledge Format concept.
	Format Format

	// Metadata is how a FormatMark document spells the metadata that names
	// its page: MetadataComments, or "", for HTML comment headers, or
	// MetadataFrontMatter for YAML front matter, which mark publishes only
	// with the frontmatter feature, or MetadataNone for the body alone.
	// FormatOKF writes front matter of its own and ignores it.
	Metadata Metadata

	// Generator is who an OKF concept says generated it, as
	// <producer>/<version>; empty is "mark/unknown".
	Generator string

	// Documents are the other pages exported with this one. A link to one of
	// them, or a URL of one pasted into the body, is written as a relative link
	// to its file. Nil writes every page link as ac:Title.
	Documents *Documents
}

// PageFile is a page exported with others, and the file it goes to.
type PageFile struct {
	ID, Title, Output string
}

// Documents finds the pages of an export by title, as a page link names one,
// or by id, as most URLs do.
type Documents struct {
	byTitle map[string]PageFile
	byID    map[string]PageFile
}

// NewDocuments indexes the pages of an export, all of one space.
func NewDocuments(documents []PageFile) *Documents {
	index := &Documents{
		byTitle: make(map[string]PageFile, len(documents)),
		byID:    make(map[string]PageFile, len(documents)),
	}
	for _, d := range documents {
		// Titles are unique in a space without regard to case.
		index.byTitle[strings.ToLower(d.Title)] = d
		index.byID[d.ID] = d
	}
	return index
}

// Result is what Page wrote.
type Result struct {
	// Page is the page exported.
	Page *confluence.PageInfo

	// Title is the page's title.
	Title string

	// Markdown is the document, as written to Output or Stdout: the body
	// below its metadata, or the body alone under MetadataNone. A caller that
	// only wants the text can read it here and discard Stdout.
	Markdown string

	// Output is the file the Markdown was written to, or "" for Stdout.
	Output string

	// Attachments are the files the attachments were written to.
	Attachments []string

	// Labels are the page's global labels, sorted: the Label headers, or
	// the tags of OKF front matter. Nil when the page has none. Each is the
	// label's whole name as Confluence keeps it; mark gives no meaning to
	// any character in one, "=" included, so a key=value convention is the
	// caller's to split.
	Labels []string

	// DeclaredAttachments are the attachments named by Attachment headers,
	// in order, or that would be under MetadataNone, which writes no headers.
	// Nil when there are none, as under OKF, which declares none.
	DeclaredAttachments []string
}

// expansions are what an exported page is read with.
const expansions = "body.storage,ancestors,version,space"

// Page exports a Confluence page to a Markdown file that mark can publish
// back: its body converted to Markdown, below the metadata headers that name
// the page -- its space, its ancestry, its title, its labels -- and with the
// attachments it refers to written beside it.
//
// With FormatOKF it is written as an Open Knowledge Format concept instead:
// the same body below OKF front matter, for reading rather than publishing.
func Page(ctx context.Context, api *confluence.API, cfg Config) (*Result, error) {
	export, err := preparePage(ctx, api, cfg)
	if err != nil {
		return nil, err
	}
	if !cfg.Overwrite {
		if err := refuseExisting(export.targets()); err != nil {
			return nil, err
		}
	}
	return export.write(ctx, api)
}

// pageExport is a page read and converted, with the files it is written to,
// none of them written yet.
type pageExport struct {
	page      *confluence.PageInfo
	output    string
	markdown  string
	downloads []pendingDownload
	stdout    io.Writer
	labels    []string
	declared  []string
}

// targets are the files the export writes.
func (e *pageExport) targets() []string {
	targets := make([]string, 0, len(e.downloads)+1)
	if e.output != "" {
		targets = append(targets, e.output)
	}
	for _, d := range e.downloads {
		targets = append(targets, d.path)
	}
	return targets
}

// preparePage reads and converts the page cfg names and works out every file
// its export writes, without writing any.
func preparePage(ctx context.Context, api *confluence.API, cfg Config) (*pageExport, error) {
	format, spelling, err := parseOptions(cfg)
	if err != nil {
		return nil, err
	}
	okf := format == FormatOKF

	// OKF reads these names as a directory's index and log at any level.
	if okf && cfg.Output != "" && cfg.Output != "-" {
		if name := filepath.Base(cfg.Output); okfReservedFile(name) {
			return nil, fmt.Errorf("%s cannot hold an OKF concept: OKF reserves the name %s", cfg.Output, name)
		}
	}

	page, err := findPage(ctx, api, cfg)
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

	names := newLocalNames()
	names.okf = okf
	// An attachment named like the document would be written over it, or it
	// over the attachment; on macOS and Windows, also when only case differs.
	if output != "" && sameDir(outputDir, attachmentsDir) {
		names.reserve(filepath.Base(output))
	}
	// Worked out only for a page that has attachments: on Windows there is no
	// relative path to another drive.
	var relative *string
	pathOf := func(filename string) string {
		if relative == nil {
			dir := referTo(outputDir, attachmentsDir)
			relative = &dir
		}
		return filepath.ToSlash(filepath.Join(*relative, names.of(filename)))
	}

	var pagePath func(string) (string, bool)
	var pageURLPath func(string) (string, string, bool)
	if cfg.Documents != nil {
		relativePath := func(d PageFile) (string, bool) {
			rel, err := filepath.Rel(outputDir, d.Output)
			if err != nil {
				return "", false
			}
			return filepath.ToSlash(rel), true
		}

		pagePath = func(title string) (string, bool) {
			d, ok := cfg.Documents.byTitle[strings.ToLower(title)]
			if !ok {
				return "", false
			}
			return relativePath(d)
		}

		// Confluence's idea of its own address and the one mark was given can
		// differ, behind a proxy or on a custom domain; a URL may be on either.
		instances := []string{api.BaseURL()}
		if strings.HasPrefix(page.Links.Base, "http") {
			instances = append(instances, page.Links.Base)
		}
		pageURLPath = func(href string) (string, string, bool) {
			var ref PageRef
			ok := false
			for _, instance := range instances {
				if ref, ok = pageOnInstance(href, instance); ok {
					break
				}
			}
			if !ok {
				return "", "", false
			}

			var d PageFile
			switch {
			case ref.PageID != "":
				d, ok = cfg.Documents.byID[ref.PageID]
			case strings.EqualFold(ref.Space, space):
				d, ok = cfg.Documents.byTitle[strings.ToLower(ref.Title)]
			default:
				ok = false
			}
			if !ok {
				return "", "", false
			}

			path, ok := relativePath(d)
			return path, d.Title, ok
		}
	}

	var pageAddress func(string, string) (string, bool)
	var lookupErr error
	if okf {
		pageAddress = pageURLs(ctx, api, space, &lookupErr)
	}

	doc, err := Convert(page.Body.Storage.Value, Options{
		Space: space, Title: page.Title, AttachmentPath: pathOf, PagePath: pagePath, PageURL: pageAddress, PageURLPath: pageURLPath,
		Reading: okf, FrontMatter: !okf && spelling == MetadataFrontMatter,
		NoMetadata: !okf && spelling == MetadataNone,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to convert page %s: %w", page.ID, err)
	}
	if lookupErr != nil {
		return nil, fmt.Errorf("unable to convert page %s: %w", page.ID, lookupErr)
	}

	// Without the attachments, the page's are not asked for: every one the
	// document refers to is declared, as it would be were they all there.
	var downloads []pendingDownload
	var missing map[string]bool
	if !cfg.NoAttachments {
		downloads, missing, err = attachmentsToWrite(ctx, api, page, doc, attachmentsDir, names, pathOf)
		if err != nil {
			return nil, err
		}
	}

	labels, err := pageLabels(ctx, api, page)
	if err != nil {
		return nil, err
	}

	var header string
	var declared []string
	if okf {
		header, err = conceptHeader(page, pageURL(api, page), cfg.Generator, labels)
	} else {
		// A header naming a file that is not there fails the publish.
		for _, name := range doc.Declared {
			if !missing[name] {
				declared = append(declared, name)
			}
		}
		header, err = headers(ctx, api, page, space, doc, labels, declared, spelling)
	}
	if err != nil {
		return nil, err
	}

	markdown := header
	if header != "" && doc.Markdown != "" {
		markdown += "\n"
	}
	markdown += doc.Markdown

	return &pageExport{
		page: page, output: output, markdown: markdown, downloads: downloads, stdout: cfg.Stdout,
		labels: labels, declared: declared,
	}, nil
}

// write downloads the attachments and writes the document, over whatever is
// there: refusing existing files is the caller's.
func (e *pageExport) write(ctx context.Context, api *confluence.API) (*Result, error) {
	result := &Result{
		Page: e.page, Title: e.page.Title, Markdown: e.markdown, Output: e.output,
		Labels: e.labels, DeclaredAttachments: e.declared,
	}

	written, err := downloadAll(ctx, api, e.page.ID, e.downloads)
	if err != nil {
		return nil, err
	}
	result.Attachments = written

	if e.output == "" {
		if _, err := io.WriteString(e.stdout, e.markdown); err != nil {
			return nil, fmt.Errorf("unable to write the page: %w", err)
		}
		return result, nil
	}

	dir := filepath.Dir(e.output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("unable to create %s: %w", dir, err)
	}
	if err := os.WriteFile(e.output, []byte(e.markdown), 0o644); err != nil { //nolint:gosec // G306: a document, meant to be read like the others beside it
		return nil, fmt.Errorf("unable to write %s: %w", e.output, err)
	}

	log.Info().Msgf("page %q exported to %s", e.page.Title, e.output)

	return result, nil
}

// findPage reads the page the configuration names, with everything an export
// needs of it.
func findPage(ctx context.Context, api *confluence.API, cfg Config) (*confluence.PageInfo, error) {
	id := cfg.PageID

	if id == "" {
		if cfg.Space == "" || cfg.Title == "" {
			return nil, errors.New("a page is named by its id, or by its space and title")
		}

		var found *confluence.PageInfo
		for _, pageType := range []confluence.ContentType{confluence.ContentPage, confluence.ContentBlogPost} {
			var err error
			found, err = api.FindPage(ctx, cfg.Space, cfg.Title, pageType)
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

	page, err := api.GetPageByIDExpanded(ctx, id, expansions)
	if err != nil {
		return nil, fmt.Errorf("unable to read page %s: %w", id, err)
	}

	return page, nil
}

// headers writes the metadata that publishes the document back to the page it
// came from, spelled as MetadataComments or MetadataFrontMatter says, or
// nothing under MetadataNone, without the lookups only the metadata needs.
func headers(
	ctx context.Context,
	api *confluence.API,
	page *confluence.PageInfo,
	space string,
	doc *Document,
	labels, declared []string,
	spelling Metadata,
) (string, error) {
	if spelling == MetadataNone {
		return "", nil
	}

	h := pageHeaders{Space: space, Title: page.Title, Labels: labels, Attachments: declared}

	var err error
	if page.Type == confluence.ContentBlogPost {
		h.Type = "blogpost"
	} else {
		h.Parents, h.Folders, err = parents(ctx, api, page, space)
		if err != nil {
			return "", err
		}
	}

	// A sidebar implies the article layout, so only one of them is said.
	if doc.Sidebar != "" {
		h.Sidebar = doc.Sidebar
	} else {
		h.Layout = doc.Layout
	}

	h.ContentAppearance, h.Emoji = appearanceOf(ctx, api, page)

	if spelling == MetadataFrontMatter {
		return h.frontMatter()
	}
	return h.comments()
}

// conceptHeader is the OKF front matter of the concept page is exported as.
func conceptHeader(page *confluence.PageInfo, address, generator string, labels []string) (string, error) {
	if generator == "" {
		generator = "mark/unknown"
	}
	return okfFrontMatter(page, address, generator, labels)
}

// pageLabels are the page's global labels, sorted, so that exporting a page
// twice writes the same file.
func pageLabels(ctx context.Context, api *confluence.API, page *confluence.PageInfo) ([]string, error) {
	labels, err := api.GetPageLabels(ctx, page, "global")
	if err != nil {
		return nil, fmt.Errorf("unable to read the labels of page %s: %w", page.ID, err)
	}
	var names []string
	for _, label := range labels.Labels {
		names = append(names, label.Name)
	}
	sort.Strings(names)
	return names, nil
}

// pageURLs looks up the address of a page by its space ("" for space) and
// title; the API caches the answers. A page that is not there has none, and
// neither has one in a space that is not there, which through the gateway is
// also a space the token cannot see. The first lookup that fails otherwise is
// kept in failed, for the caller to report, since the link would point nowhere
// although the page may exist; once one has failed the export fails anyway,
// so no more are made.
func pageURLs(ctx context.Context, api *confluence.API, space string, failed *error) func(string, string) (string, bool) {
	return func(in, title string) (string, bool) {
		if *failed != nil {
			return "", false
		}
		in = cmp.Or(in, space)
		page, err := api.FindPage(ctx, in, title, confluence.ContentPage)
		if errors.Is(err, confluence.ErrNotFound) {
			return "", false
		}
		if err != nil {
			*failed = fmt.Errorf("unable to look up page %q in space %s: %w", title, in, err)
			return "", false
		}
		if page == nil {
			return "", false
		}
		address := pageURL(api, page)
		return address, address != ""
	}
}

// parents is the page's ancestry below the space's home page, which is where
// mark starts from when it resolves Parent headers, as the titles of the pages
// and of the folders beneath them.
//
// mark puts Folder headers beneath the pages the Parent headers name, so only
// folders directly above the page can be said. One higher up is left out, with
// a warning: the page is found by its parent's title, which still puts it in
// the folder unless that parent is moved.
func parents(ctx context.Context, api *confluence.API, page *confluence.PageInfo, space string) (pages, folders []string, err error) {
	chain := ancestry(ctx, api, page)
	if len(chain) == 0 {
		return nil, nil, nil
	}

	home, err := api.FindHomePage(ctx, space)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to find the home page of space %s: %w", space, err)
	}

	belowHome := home != nil && chain[0].ID == home.ID
	if belowHome {
		chain = chain[1:]
	} else if chain[0].Type != confluence.ContentFolder {
		log.Warn().Msgf(
			"page %q is not below the home page of space %s; publishing it back puts it there",
			page.Title, space,
		)
	}

	for _, ancestor := range chain {
		if ancestor.Type == confluence.ContentFolder {
			folders = append(folders, ancestor.Title)
			continue
		}
		if len(folders) > 0 {
			log.Warn().Msgf(
				"page %q is below folder %q, with page %q between them, and Folder headers can only name "+
					"the folders directly above a page: the folder is left out of its headers",
				page.Title, folders[len(folders)-1], ancestor.Title,
			)
			return pageTitles(chain), nil, nil
		}
		pages = append(pages, ancestor.Title)
	}

	// Folder headers with no Parent look for the folders at the root of the
	// space, not beneath the home page.
	if belowHome && len(pages) == 0 && len(folders) > 0 {
		pages = []string{home.Title}
	}

	return pages, folders, nil
}

// pageTitles are the titles of the pages in chain.
func pageTitles(chain []confluence.Ancestor) []string {
	var titles []string
	for _, ancestor := range chain {
		if ancestor.Type != confluence.ContentFolder {
			titles = append(titles, ancestor.Title)
		}
	}
	return titles
}

// ancestry is the chain of pages and folders above page, root first. v1's
// ancestors leave folders out, so where there can be folders, on Cloud, the
// chain is read from v2 instead; where it cannot be read, a warning says the
// folders may be missing.
func ancestry(ctx context.Context, api *confluence.API, page *confluence.PageInfo) []confluence.Ancestor {
	v1 := make([]confluence.Ancestor, 0, len(page.Ancestors))
	for _, ancestor := range page.Ancestors {
		v1 = append(v1, confluence.Ancestor{ID: ancestor.ID, Title: ancestor.Title, Type: confluence.ContentPage})
	}

	if isCloud, err := api.Cloud(ctx); err == nil && !isCloud {
		return v1
	}

	chain, err := api.AncestryV2(ctx, page.ID)
	if err != nil {
		log.Warn().Msgf(
			"unable to tell whether page %q is in a folder, so its headers name no folder: %s", page.Title, err,
		)
		return v1
	}

	return chain
}

// appearanceOf reads the content appearance and the title emoji of a page,
// which Confluence keeps as content properties. Neither is worth failing the
// export over: a token that may not read properties exports the page without
// them, and says so.
func appearanceOf(ctx context.Context, api *confluence.API, page *confluence.PageInfo) (appearance metadata.Appearance, emoji string) {
	properties, err := api.ContentProperties(ctx, page.ID)
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
			appearance = metadata.Appearance(value)
		case "emoji-title-published":
			emoji = emojiFromHex(value)
		}
	}

	switch appearance {
	case metadata.FixedContentAppearance:
		// An older editor's narrow page is the narrow page mark publishes.
		appearance = metadata.DefaultContentAppearance
	case metadata.FullWidthContentAppearance:
		// mark publishes full width unless told otherwise.
		appearance = ""
	default:
		if _, err := metadata.ParseContentAppearance(string(appearance)); err != nil {
			log.Warn().Msgf("page %q has the content appearance %q, which mark does not know; publishing it back gives it the default width",
				page.Title, appearance)
			appearance = ""
		}
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

// referTo is how the Markdown file, in base, refers to the directory target:
// relative to it where it can be, and absolute where it cannot, such as on
// another drive.
func referTo(base, target string) string {
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return target
	}

	absoluteBase, err := filepath.Abs(base)
	if err != nil {
		return absoluteTarget
	}

	relative, err := filepath.Rel(absoluteBase, absoluteTarget)
	if err != nil {
		return absoluteTarget
	}

	return relative
}

// sameDir reports whether two paths name one directory, without regard to
// case: a name reserved in a directory that only differs in case costs a
// number, where missing one costs a file.
func sameDir(a, b string) bool {
	absoluteA, errA := filepath.Abs(a)
	absoluteB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return true
	}
	return strings.EqualFold(absoluteA, absoluteB)
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

// localNames gives every attachment a file of its own. Names that flatten to
// the same local name, or differ only in case and so share a file on macOS and
// Windows, are told apart by a number before the extension. A name longer
// than a file name may be is cut before its extension.
type localNames struct {
	byName map[string]string
	taken  map[string]bool
	okf    bool
}

func newLocalNames() *localNames {
	return &localNames{byName: map[string]string{}, taken: map[string]bool{}}
}

// reserve keeps a name from every attachment.
func (l *localNames) reserve(name string) {
	l.taken[strings.ToLower(name)] = true
}

func (l *localNames) of(filename string) string {
	if name, ok := l.byName[filename]; ok {
		return name
	}

	name := localName(filename)
	if l.okf {
		name = okfName(name)
	}
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	// A name cut short is marked with a hash of the whole one, so that names
	// sharing a long prefix keep files of their own whatever their order.
	mark := ""
	if len(name) > nameMax {
		mark = "-" + nameHash(filename)
		name = fitName(stem, mark, extension)
	}
	for i := 2; l.taken[strings.ToLower(name)]; i++ {
		name = fitName(stem, fmt.Sprintf("%s-%d", mark, i), extension)
	}

	l.byName[filename] = name
	l.taken[strings.ToLower(name)] = true

	return name
}

// nameMax is the longest file name, in bytes, that most filesystems take.
const nameMax = 255

// fitName joins a stem, a suffix and an extension, cutting the stem on a
// character's boundary to keep the name within nameMax bytes. An extension
// too long to leave the stem room is cut with it.
func fitName(stem, suffix, extension string) string {
	if len(stem)+len(suffix)+len(extension) <= nameMax {
		return stem + suffix + extension
	}
	if len(extension) > nameMax/2 {
		stem, extension = stem+extension, ""
	}

	return cutBytes(stem, nameMax-len(suffix)-len(extension)) + suffix + extension
}

// cutBytes is the longest start of s, at most n bytes, that ends on a
// character's boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n]
}

// nameHash is a short, stable hash of an attachment's name.
func nameHash(filename string) string {
	sum := sha256.Sum256([]byte(filename))
	return fmt.Sprintf("%x", sum[:4])
}

type pendingDownload struct {
	info confluence.AttachmentInfo
	path string
}

// attachmentsToWrite pairs each attachment the body refers to with the file
// it goes to, and says which of them the page does not have, by the path the
// document refers to them with.
func attachmentsToWrite(
	ctx context.Context,
	api *confluence.API,
	page *confluence.PageInfo,
	doc *Document,
	dir string,
	names *localNames,
	pathOf func(string) string,
) ([]pendingDownload, map[string]bool, error) {
	if len(doc.Attachments) == 0 {
		return nil, nil, nil
	}

	remote, err := api.GetAttachments(ctx, page.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to list the attachments of page %s: %w", page.ID, err)
	}

	byName := make(map[string]confluence.AttachmentInfo, len(remote))
	for _, info := range remote {
		byName[info.Filename] = info
	}

	var downloads []pendingDownload
	missing := map[string]bool{}
	for _, name := range doc.Attachments {
		info, ok := byName[name]
		if !ok {
			log.Warn().Msgf("page %q refers to an attachment %q it does not have", page.Title, name)
			missing[pathOf(name)] = true
			continue
		}
		downloads = append(downloads, pendingDownload{info: info, path: filepath.Join(dir, names.of(name))})
	}

	return downloads, missing, nil
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

// downloadAll downloads every attachment to a temporary file beside its own,
// and only once all of them are there renames them into place, so that a
// failed download leaves nothing behind.
func downloadAll(ctx context.Context, api *confluence.API, pageID string, downloads []pendingDownload) ([]string, error) {
	var temporaries []string
	defer func() {
		for _, name := range temporaries {
			_ = os.Remove(name)
		}
	}()

	for _, d := range downloads {
		name, err := downloadTemporary(ctx, api, pageID, d)
		if err != nil {
			return nil, err
		}
		temporaries = append(temporaries, name)
	}

	written := make([]string, 0, len(downloads))
	for i, d := range downloads {
		if err := os.Rename(temporaries[i], d.path); err != nil {
			return written, fmt.Errorf("unable to write attachment %q to %s: %w", d.info.Filename, d.path, err)
		}
		log.Info().Msgf("attachment %q written to %s", d.info.Filename, d.path)
		written = append(written, d.path)
	}

	return written, nil
}

// downloadTemporary downloads an attachment to a temporary file in the
// directory it goes to, and returns the file's name.
func downloadTemporary(ctx context.Context, api *confluence.API, pageID string, d pendingDownload) (string, error) {
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("unable to create %s: %w", dir, err)
	}

	temporary, err := os.CreateTemp(dir, ".mark-export-*")
	if err != nil {
		return "", fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, err)
	}

	err = api.DownloadAttachment(ctx, pageID, d.info, temporary)
	if closeErr := temporary.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, closeErr)
	}
	if err == nil {
		if chmodErr := os.Chmod(temporary.Name(), 0o644); chmodErr != nil { //nolint:gosec // G302: a document's image, meant to be read like it
			err = fmt.Errorf("unable to write attachment %q: %w", d.info.Filename, chmodErr)
		}
	}
	if err != nil {
		_ = os.Remove(temporary.Name())
		return "", err
	}

	return temporary.Name(), nil
}
