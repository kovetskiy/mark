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

// Tree exports a page and the pages beneath it into the directory
// cfg.Output names, depth levels deep counting the page itself; 0 is every
// level. cfg names the page as it does for Page; its AttachmentsDir is
// refused, since every page's attachments go beside it.
//
// The page is written to <Output>/index.md, and every page beneath it to
// index.md in a directory of its own named after its title, inside its
// parent's: mark names an attachment after its path, so two pages sharing
// a directory would also share, and overwrite, an image of the same name.
// Each document carries the headers that publish it back where it was,
// whichever directory it is in.
//
// The tree is read and every document's path checked before anything is
// written. A folder in it is refused: a page's Parent headers are read from
// ancestors that leave folders out, so the pages inside one would be
// published back outside it.
func Tree(ctx context.Context, api *confluence.API, cfg Config, depth int) ([]*Result, error) {
	if cfg.Output == "" || cfg.Output == "-" {
		return nil, errors.New("a recursive export writes a directory: name it with --output")
	}
	if cfg.AttachmentsDir != "" {
		return nil, errors.New("a recursive export writes each page's attachments beside it, so --attachments-dir does not apply")
	}

	root, err := findPage(api, cfg)
	if err != nil {
		return nil, err
	}
	if root.Type == "blogpost" {
		return nil, fmt.Errorf("%q (%s) is a blog post, and a blog post has no pages beneath it", root.Title, root.ID)
	}

	var plan []planned
	if err := planTree(api, confluence.Child{ID: root.ID, Type: "page", Title: root.Title}, cfg.Output, depth, &plan); err != nil {
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

	results := make([]*Result, 0, len(plan))
	for _, p := range plan {
		result, err := Page(ctx, api, Config{
			PageID:        p.id,
			Output:        p.output,
			Overwrite:     cfg.Overwrite,
			NoAttachments: cfg.NoAttachments,
			Stdout:        cfg.Stdout,
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
	output string
}

// planTree adds the page and, while depth allows, what is beneath it, to
// plan: the page's document in dir, each child's in a directory of its own
// inside dir.
func planTree(api *confluence.API, page confluence.Child, dir string, depth int, plan *[]planned) error {
	*plan = append(*plan, planned{id: page.ID, output: filepath.Join(dir, TreeDocument)})
	if depth == 1 {
		return nil
	}

	children, err := api.Children(page.ID, "page")
	if err != nil {
		return fmt.Errorf("unable to list the children of %q (%s): %w", page.Title, page.ID, err)
	}

	next := 0
	if depth > 0 {
		next = depth - 1
	}

	taken := map[string]bool{}
	for _, child := range children {
		switch child.Type {
		case "page":
		case "folder":
			return fmt.Errorf(
				"folder %q (%s) is beneath %q (%s), and a recursive export does not reach into folders: "+
					"the pages inside one would be published back outside it",
				child.Title, child.ID, page.Title, page.ID,
			)
		default:
			log.Warn().Msgf("skipping %s %q (%s): only pages are exported", child.Type, child.Title, child.ID)
			continue
		}

		name := slug(child.Title)
		if name == "" || taken[name] {
			name = strings.TrimPrefix(name+"-"+child.ID, "-")
		}
		taken[name] = true

		if err := planTree(api, child, filepath.Join(dir, name), next, plan); err != nil {
			return err
		}
	}

	return nil
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
