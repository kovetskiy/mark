package page

import (
	"fmt"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// ImmediateParentID returns the direct parent content ID from expanded ancestors, or "" if unknown.
func ImmediateParentID(pg *confluence.PageInfo) string {
	if pg == nil || len(pg.Ancestors) == 0 {
		return ""
	}
	return pg.Ancestors[len(pg.Ancestors)-1].ID
}

// ParentIDFor returns the id of a page's direct parent, preferring what
// Confluence expanded and falling back to the parent this run resolved.
//
// A page under a folder has no expanded ancestors -- folders are not pages and
// never appear in that chain -- so the ancestors alone answer "" for every
// folder-parented page. Everything that needs a parent id then quietly did
// nothing: a document declaring both Folder and Order got neither the ordering
// nor a word about it.
func ParentIDFor(pg *confluence.PageInfo, resolved *confluence.PageInfo) string {
	if id := ImmediateParentID(pg); id != "" {
		return id
	}

	if resolved != nil {
		return resolved.ID
	}

	return ""
}

// UnderDeclaredParents reports whether a page sits under the parents its
// headers declare.
//
// Loose on purpose, and loose in the same way ValidateAncestry is: a page nested
// below its declared parent still counts, because every parent the headers name
// is in its ancestry and nothing has been contradicted. Tightening this would
// move pages whose placement nobody complained about.
func UnderDeclaredParents(pg *confluence.PageInfo, parents []string) bool {
	return pageUnderParents(pg, parents)
}

// offAnchor reports whether a page that a document with folders found by title
// sits somewhere other than under the document's MARK_PARENTS, and so is a
// different page that happens to share the title.
//
// An empty ancestor chain is no evidence of that. It is what v1 answers for a
// page whose parent is a folder -- folders never appear in the chain -- which is
// exactly what the page this document published last time looks like. Reading
// it as off-anchor sent every run without --track-pages to create the page a
// second time, and Confluence refuses that: a title is unique within a space.
// The one parentless page that is certainly not the document's is the space
// home page, and that is still passed over, as is any page when the home page
// cannot be looked up to tell.
func offAnchor(api *confluence.API, space string, pg *confluence.PageInfo, parents []string) bool {
	if len(pg.Ancestors) > 0 {
		return !pageUnderParents(pg, parents)
	}

	homepage, err := api.FindHomePage(space)
	if err != nil || homepage == nil {
		return true
	}

	return homepage.ID == pg.ID
}

// pageUnderParents reports whether any ancestor title matches one of the MARK_PARENTS chain.
func pageUnderParents(pg *confluence.PageInfo, parents []string) bool {
	if pg == nil || len(parents) == 0 {
		return true
	}
	parentSet := make(map[string]struct{}, len(parents))
	for _, p := range parents {
		parentSet[p] = struct{}{}
	}
	for _, a := range pg.Ancestors {
		if _, ok := parentSet[a.Title]; ok {
			return true
		}
	}
	return false
}

// EnsurePageUnderFolderParent is EnsurePageUnderParent with folderID as the
// parent; it does nothing folder-specific, since Confluence moves a page under
// a folder the same way it moves one under a page.
func EnsurePageUnderFolderParent(
	api *confluence.API,
	pg *confluence.PageInfo,
	folderID string,
) error {
	return EnsurePageUnderParent(api, pg, folderID)
}

// EnsurePageUnderParent moves an existing page under parentID when its direct
// parent differs. The parent may be a page or a folder; Confluence moves
// content the same way either way.
func EnsurePageUnderParent(
	api *confluence.API,
	pg *confluence.PageInfo,
	parentID string,
) error {
	if pg == nil || parentID == "" {
		return nil
	}

	currentParent := ImmediateParentID(pg)
	if currentParent == parentID {
		return nil
	}

	log.Info().Msgf(
		"moving page %q (%s) from parent %s to %s",
		pg.Title,
		pg.ID,
		currentParent,
		parentID,
	)

	if err := api.MoveContentAppend(pg.ID, parentID); err != nil {
		return fmt.Errorf("move page %q under new parent: %w", pg.Title, err)
	}

	refreshed, err := api.GetPageByID(pg.ID)
	if err != nil {
		return fmt.Errorf("refresh page %q after move: %w", pg.Title, err)
	}
	if refreshed == nil {
		return fmt.Errorf("page %q not found after move", pg.Title)
	}

	pageType := pg.Type
	*pg = *refreshed
	if pg.Type == "" {
		if pageType != "" {
			pg.Type = pageType
		} else {
			pg.Type = "page"
		}
	}
	return nil
}
