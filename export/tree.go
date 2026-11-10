package export

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/rs/zerolog/log"
)

// TreeDocument is the name of the file each page of a tree is written to,
// inside a directory of its own.
const TreeDocument = "index.md"

// ErrTreeWithoutMetadata refuses MetadataNone for a tree of FormatMark
// documents: a tree is exported to be published back where it was, which
// takes the metadata that names each page.
var ErrTreeWithoutMetadata = errors.New("a recursive export writes documents that publish back where they were, " +
	"so --metadata none does not apply")

// TreeOptions are what Tree adds to the Config of a single page.
type TreeOptions struct {
	// Depth is how many levels to export, counting the page itself; 0 is
	// every level.
	Depth int

	// RelativeLinks writes a link between two pages of the tree as a relative
	// link between their documents, which a repository host can follow,
	// rather than as [text](ac:Title). So does a URL of such a page pasted
	// into a body: a short link, or the address a browser shows. Published
	// back, either becomes a tiny link to the page.
	RelativeLinks bool
}

// Tree exports a page and the pages beneath it into the directory
// cfg.Output names. cfg names the page as it does for Page; with no page but
// a Space it exports the whole space, from its top-level pages down. Its
// AttachmentsDir is refused, since every page's attachments go beside it.
//
// The page, or the space's home page, is written to <Output>/index.md, and
// every page beneath it to index.md in a directory of its own named after its
// title, inside its parent's: mark names an attachment after its path, so two
// pages sharing a directory would also share, and overwrite, an image of the
// same name. A top-level page other than the home page gets a directory
// beside the home page's children. Each document carries the headers that
// publish it back where it was, whichever directory it is in.
//
// With FormatOKF the tree is an Open Knowledge Format bundle instead: each
// page a concept, <slug>.md, with the pages beneath it in the directory <slug>
// beside it; an index.md in every directory listing what is in it, the
// bundle's own declaring the OKF version; every page's attachments in
// references/attachments/<page id>; and links between the pages relative
// links, whatever RelativeLinks says.
//
// The tree is read, every page converted, and every path it writes to --
// documents, indexes, attachments -- checked before anything is written. A
// folder in it is refused, since the export does not list a folder's pages.
// MetadataNone is refused with ErrTreeWithoutMetadata, except under FormatOKF,
// which ignores the metadata.
func Tree(ctx context.Context, api *confluence.API, cfg Config, options TreeOptions) ([]*Result, error) {
	format, spelling, err := parseOptions(cfg)
	if err != nil {
		return nil, err
	}
	l := layout{okf: format == FormatOKF}
	if !l.okf && spelling == MetadataNone {
		return nil, ErrTreeWithoutMetadata
	}

	if cfg.Output == "" || cfg.Output == "-" {
		return nil, errors.New("a recursive export writes a directory: name it with --output")
	}
	if cfg.AttachmentsDir != "" {
		return nil, errors.New("a recursive export writes each page's attachments beside it, so --attachments-dir does not apply")
	}

	var plan []planned
	heading := cfg.Space
	if cfg.PageID == "" && cfg.Title == "" {
		plan, err = planSpace(ctx, api, l, cfg.Space, cfg.Output, options.Depth)
	} else {
		plan, err = planPage(ctx, api, l, cfg, options.Depth)
		if len(plan) > 0 {
			heading = plan[0].title
		}
	}
	if err != nil {
		return nil, err
	}

	var indexes map[string]string
	if l.okf {
		indexes = okfIndexes(cfg.Output, heading, plan)
	}

	var documents *Documents
	if options.RelativeLinks || l.okf {
		listed := make([]PageFile, len(plan))
		for i, p := range plan {
			listed[i] = PageFile{ID: p.id, Title: p.title, Output: p.output}
		}
		documents = NewDocuments(listed)
	}

	// Every page is converted before any is written, so that an existing
	// attachment of the last page is refused as early as a document.
	exports := make([]*pageExport, 0, len(plan))
	targets := slices.Collect(maps.Keys(indexes))
	for _, p := range plan {
		var attachments string
		if l.okf {
			attachments = filepath.Join(cfg.Output, okfReferences, "attachments", p.id)
		}

		export, err := preparePage(ctx, api, Config{
			PageID:         p.id,
			Output:         p.output,
			AttachmentsDir: attachments,
			NoAttachments:  cfg.NoAttachments,
			Stdout:         cfg.Stdout,
			Format:         format,
			Metadata:       spelling,
			Generator:      cfg.Generator,
			Documents:      documents,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to export page %s: %w", p.id, err)
		}
		exports = append(exports, export)
		targets = append(targets, export.targets()...)
	}

	if !cfg.Overwrite {
		if err := refuseExisting(targets); err != nil {
			return nil, err
		}
	}

	results := make([]*Result, 0, len(exports))
	for _, export := range exports {
		result, err := export.write(ctx, api)
		if err != nil {
			return results, fmt.Errorf("unable to export page %s, after exporting %d: %w", export.page.ID, len(results), err)
		}
		results = append(results, result)
	}

	if err := writeIndexes(indexes); err != nil {
		return results, err
	}

	return results, nil
}

// layout is where a tree puts each page's document.
type layout struct{ okf bool }

// document is the file a page called name, in dir, is written to, and the
// directory the pages beneath it go in: name/index.md and name for mark,
// name.md and name for OKF.
func (l layout) document(dir, name string) (output, below string) {
	if l.okf {
		return filepath.Join(dir, name+".md"), filepath.Join(dir, name)
	}
	return filepath.Join(dir, name, TreeDocument), filepath.Join(dir, name)
}

// names are the names taken in a directory before any page is named: none
// for mark; for OKF, the index and log, and at the root of the tree the
// references directory the bundle keeps attachments in.
func (l layout) names(atRoot bool) map[string]bool {
	taken := map[string]bool{}
	if !l.okf {
		return taken
	}
	for _, name := range okfReserved {
		taken[name] = true
	}
	if atRoot {
		taken[okfReferences] = true
	}
	return taken
}

// writeIndexes writes an OKF bundle's index files.
func writeIndexes(indexes map[string]string) error {
	for _, path := range slices.Sorted(maps.Keys(indexes)) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("unable to create the directory of %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(indexes[path]), 0o644); err != nil { //nolint:gosec // G306: a document, meant to be read like the others beside it
			return fmt.Errorf("unable to write %s: %w", path, err)
		}
	}
	return nil
}

type planned struct {
	id     string
	title  string
	output string
}

// branch is a page to plan below a directory, and how many levels of it.
type branch struct {
	confluence.Child
	depth int
}

func planPage(ctx context.Context, api *confluence.API, l layout, cfg Config, depth int) ([]planned, error) {
	root, err := findPage(ctx, api, cfg)
	if err != nil {
		return nil, err
	}
	if root.Type == confluence.ContentBlogPost {
		return nil, fmt.Errorf("%q (%s) is a blog post, and a blog post has no pages beneath it", root.Title, root.ID)
	}

	page := confluence.Child{ID: root.ID, Type: confluence.ContentPage, Title: root.Title}

	var plan []planned
	if l.okf {
		// A concept is never an index.md, so the page is named like any other.
		err = planBranches(ctx, api, l, []branch{{Child: page, depth: depth}}, cfg.Output, l.names(true), &plan)
	} else {
		err = planTree(ctx, api, l, page, filepath.Join(cfg.Output, TreeDocument), cfg.Output, depth, &plan)
	}
	return plan, err
}

// planSpace plans the whole of a space: the home page as the tree's own
// page, and every other top-level page beside the home page's children.
func planSpace(ctx context.Context, api *confluence.API, l layout, space, dir string, depth int) ([]planned, error) {
	if space == "" {
		return nil, errors.New("a recursive export needs a page, or a space to export the whole of")
	}

	roots, err := api.RootPages(ctx, space)
	if err != nil {
		return nil, fmt.Errorf("unable to list the top-level pages of space %s: %w", space, err)
	}

	home, err := api.FindHomePage(ctx, space)
	switch {
	case errors.Is(err, confluence.ErrNoHomePage):
		home = nil
		log.Warn().Msgf("space %s has no home page: its top-level pages are exported beside each other", space)
	case err != nil:
		return nil, fmt.Errorf("unable to find the home page of space %s: %w", space, err)
	}

	var plan []planned
	var branches []branch

	if home != nil && !l.okf {
		plan = append(plan, planned{id: home.ID, title: home.Title, output: filepath.Join(dir, TreeDocument)})
		if depth != 1 {
			children, err := children(ctx, api, confluence.Child{ID: home.ID, Type: confluence.ContentPage, Title: home.Title})
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				branches = append(branches, branch{Child: child, depth: below(depth)})
			}
		}
	}

	for _, root := range roots {
		if home != nil && !l.okf {
			if root.ID == home.ID {
				continue
			}
			log.Warn().Msgf(
				"%q (%s) is at the top of space %s rather than beneath its home page: published back, it goes beneath the home page",
				root.Title, root.ID, space,
			)
		}
		branches = append(branches, branch{Child: root, depth: depth})
	}

	taken := l.names(true)
	if err := planBranches(ctx, api, l, branches, dir, taken, &plan); err != nil {
		return nil, err
	}

	// An OKF bundle has no document of its own for the home page to be, so it
	// is a top-level page like the others: where Confluence lists it, or last
	// when it is beneath another page and the export does not reach it there.
	if home != nil && l.okf && !slices.ContainsFunc(plan, func(p planned) bool { return p.id == home.ID }) {
		homeBranch := []branch{{Child: confluence.Child{ID: home.ID, Type: confluence.ContentPage, Title: home.Title}, depth: depth}}
		if err := planBranches(ctx, api, l, homeBranch, dir, taken, &plan); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

// planTree adds the page and, while depth allows, what is beneath it, to
// plan: the page's document at output, and the pages beneath it in dir.
func planTree(
	ctx context.Context, api *confluence.API, l layout, page confluence.Child, output, dir string, depth int, plan *[]planned,
) error {
	if err := checkPageID(page.ID); err != nil {
		return err
	}
	*plan = append(*plan, planned{id: page.ID, title: page.Title, output: output})
	if depth == 1 {
		return nil
	}

	children, err := children(ctx, api, page)
	if err != nil {
		return err
	}

	branches := make([]branch, len(children))
	for i, child := range children {
		branches[i] = branch{Child: child, depth: below(depth)}
	}

	return planBranches(ctx, api, l, branches, dir, l.names(false), plan)
}

// planBranches plans each branch under a name of its own inside dir: its
// title, or its title and id where taken already has the name, as another
// branch's title or a name the layout reserves.
func planBranches(
	ctx context.Context, api *confluence.API, l layout, branches []branch, dir string, taken map[string]bool, plan *[]planned,
) error {
	for _, b := range branches {
		name := slug(b.Title)
		if name == "" || taken[name] {
			name = strings.TrimPrefix(name+"-"+b.ID, "-")
		}
		// Another title can slug to the name with the id already.
		for base, n := name, 2; taken[name]; n++ {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		taken[name] = true

		output, below := l.document(dir, name)
		if err := planTree(ctx, api, l, b.Child, output, below, b.depth, plan); err != nil {
			return err
		}
	}

	return nil
}

// checkPageID refuses a page id that is not all digits, since ids become
// parts of the paths a tree is written to.
func checkPageID(id string) error {
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return fmt.Errorf("refusing page id %q: a page id is a number", id)
	}
	return nil
}

// children lists the pages beneath page, refusing a folder and skipping
// anything else that is not a page.
func children(ctx context.Context, api *confluence.API, page confluence.Child) ([]confluence.Child, error) {
	listed, err := api.Children(ctx, page.ID, confluence.ContentPage)
	if err != nil {
		return nil, fmt.Errorf("unable to list the children of %q (%s): %w", page.Title, page.ID, err)
	}

	var pages []confluence.Child
	for _, child := range listed {
		switch child.Type {
		case confluence.ContentPage:
			pages = append(pages, child)
		case confluence.ContentFolder:
			return nil, fmt.Errorf(
				"folder %q (%s) is beneath %q (%s), and a recursive export does not reach into folders",
				child.Title, child.ID, page.Title, page.ID,
			)
		default:
			log.Warn().Msgf("skipping %s %q (%s): only pages are exported", child.Type, child.Title, child.ID)
		}
	}

	return pages, nil
}

// below is the depth left for the level beneath one with depth.
func below(depth int) int {
	if depth > 0 {
		return depth - 1
	}
	return 0
}

// slugMax caps a slug in bytes. A file name may be 255 bytes, and the slug
// still takes ".md" and, beside a sibling that slugs alike, "-<id>" and
// "-<n>": 200 leaves room for all three.
const slugMax = 200

// slug is a directory name for a title: its letters and digits, lowercased,
// with a dash for each run of anything else, cut on a character's boundary
// to at most slugMax bytes. Two titles cut to the same slug are told apart
// as any two that slug alike are, by the page id.
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sep := 0
			if dash && b.Len() > 0 {
				sep = 1
			}
			if b.Len()+sep+utf8.RuneLen(r) > slugMax {
				break
			}
			if sep > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}
