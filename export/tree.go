package export

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// TreeDocument is the name of the file each page of a tree is written to,
// inside a directory of its own.
const TreeDocument = "index.md"

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
// The tree is read and every document's path checked before anything is
// written. A folder in it is refused, since the export does not list a
// folder's pages.
func Tree(ctx context.Context, api *confluence.API, cfg Config, options TreeOptions) ([]*Result, error) {
	if cfg.Output == "" || cfg.Output == "-" {
		return nil, errors.New("a recursive export writes a directory: name it with --output")
	}
	if cfg.AttachmentsDir != "" {
		return nil, errors.New("a recursive export writes each page's attachments beside it, so --attachments-dir does not apply")
	}

	var plan []planned
	var err error
	if cfg.PageID == "" && cfg.Title == "" {
		plan, err = planSpace(api, cfg.Space, cfg.Output, options.Depth)
	} else {
		plan, err = planPage(api, cfg, options.Depth)
	}
	if err != nil {
		return nil, err
	}

	if !cfg.Overwrite {
		documents := make([]string, len(plan))
		for i, p := range plan {
			documents[i] = p.output
		}
		if err := refuseExisting(documents); err != nil {
			return nil, err
		}
	}

	var documents *Documents
	if options.RelativeLinks {
		listed := make([]PageFile, len(plan))
		for i, p := range plan {
			listed[i] = PageFile{ID: p.id, Title: p.title, Output: p.output}
		}
		documents = NewDocuments(listed)
	}

	results := make([]*Result, 0, len(plan))
	for _, p := range plan {
		result, err := Page(ctx, api, Config{
			PageID:        p.id,
			Output:        p.output,
			Overwrite:     cfg.Overwrite,
			NoAttachments: cfg.NoAttachments,
			Stdout:        cfg.Stdout,
			Documents:     documents,
		})
		if err != nil {
			return results, fmt.Errorf("unable to export page %s, after exporting %d: %w", p.id, len(results), err)
		}
		results = append(results, result)
	}

	return results, nil
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

func planPage(api *confluence.API, cfg Config, depth int) ([]planned, error) {
	root, err := findPage(api, cfg)
	if err != nil {
		return nil, err
	}
	if root.Type == "blogpost" {
		return nil, fmt.Errorf("%q (%s) is a blog post, and a blog post has no pages beneath it", root.Title, root.ID)
	}

	var plan []planned
	err = planTree(api, confluence.Child{ID: root.ID, Type: "page", Title: root.Title}, cfg.Output, depth, &plan)
	return plan, err
}

// planSpace plans the whole of a space: the home page as the tree's own
// page, and every other top-level page beside the home page's children.
func planSpace(api *confluence.API, space, dir string, depth int) ([]planned, error) {
	if space == "" {
		return nil, errors.New("a recursive export needs a page, or a space to export the whole of")
	}

	roots, err := api.RootPages(space)
	if err != nil {
		return nil, fmt.Errorf("unable to list the top-level pages of space %s: %w", space, err)
	}

	home, err := api.FindHomePage(space)
	switch {
	case errors.Is(err, confluence.ErrNoHomePage):
		home = nil
		log.Warn().Msgf("space %s has no home page: its top-level pages are exported beside each other", space)
	case err != nil:
		return nil, fmt.Errorf("unable to find the home page of space %s: %w", space, err)
	}

	var plan []planned
	var branches []branch

	if home != nil {
		plan = append(plan, planned{id: home.ID, title: home.Title, output: filepath.Join(dir, TreeDocument)})
		if depth != 1 {
			children, err := children(api, confluence.Child{ID: home.ID, Type: "page", Title: home.Title})
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				branches = append(branches, branch{Child: child, depth: below(depth)})
			}
		}
	}

	for _, root := range roots {
		if home != nil && root.ID == home.ID {
			continue
		}
		if home != nil {
			log.Warn().Msgf(
				"%q (%s) is at the top of space %s rather than beneath its home page: published back, it goes beneath the home page",
				root.Title, root.ID, space,
			)
		}
		branches = append(branches, branch{Child: root, depth: depth})
	}

	if err := planBranches(api, branches, dir, &plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// planTree adds the page and, while depth allows, what is beneath it, to
// plan: the page's document in dir, each child's in a directory of its own
// inside dir.
func planTree(api *confluence.API, page confluence.Child, dir string, depth int, plan *[]planned) error {
	*plan = append(*plan, planned{id: page.ID, title: page.Title, output: filepath.Join(dir, TreeDocument)})
	if depth == 1 {
		return nil
	}

	children, err := children(api, page)
	if err != nil {
		return err
	}

	branches := make([]branch, len(children))
	for i, child := range children {
		branches[i] = branch{Child: child, depth: below(depth)}
	}

	return planBranches(api, branches, dir, plan)
}

// planBranches plans each branch in a directory of its own inside dir,
// named after its title, or after its title and id where another branch's
// title names the same directory.
func planBranches(api *confluence.API, branches []branch, dir string, plan *[]planned) error {
	taken := map[string]bool{}
	for _, b := range branches {
		name := slug(b.Title)
		if name == "" || taken[name] {
			name = strings.TrimPrefix(name+"-"+b.ID, "-")
		}
		taken[name] = true

		if err := planTree(api, b.Child, filepath.Join(dir, name), b.depth, plan); err != nil {
			return err
		}
	}

	return nil
}

// children lists the pages beneath page, refusing a folder and skipping
// anything else that is not a page.
func children(api *confluence.API, page confluence.Child) ([]confluence.Child, error) {
	listed, err := api.Children(page.ID, "page")
	if err != nil {
		return nil, fmt.Errorf("unable to list the children of %q (%s): %w", page.Title, page.ID, err)
	}

	var pages []confluence.Child
	for _, child := range listed {
		switch child.Type {
		case "page":
			pages = append(pages, child)
		case "folder":
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

// slug is a directory name for a title: its letters and digits, lowercased,
// with a dash for each run of anything else.
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
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
