package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// ImmediateParentID returns the direct parent content ID, or "" if unknown:
// the parent v2 named when the page was read from there, and otherwise the last
// of its expanded ancestors. Only the first can be a folder.
func ImmediateParentID(pg *confluence.PageInfo) string {
	if pg == nil {
		return ""
	}
	if pg.ParentID != "" {
		return pg.ParentID
	}
	if len(pg.Ancestors) == 0 {
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
//
// Every parent, though, not any one of them. A first Parent that is the home
// page is an ancestor of nearly everything in the space, so matching on any
// declared parent left a page under Home > Old whose headers had moved it to
// Home > New exactly where it was.
func UnderDeclaredParents(pg *confluence.PageInfo, parents []string) bool {
	if pg == nil {
		return true
	}

	ancestors := make(map[string]struct{}, len(pg.Ancestors))
	for _, a := range pg.Ancestors {
		ancestors[a.Title] = struct{}{}
	}
	for _, p := range parents {
		if _, ok := ancestors[p]; !ok {
			return false
		}
	}

	return true
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

// dryRunFolderID stands in for a folder a dry run would have created.
const dryRunFolderID = "dry-run-folder-id"

// WouldMove reports whether a real run would move pg, an existing page, given
// what PreviewPage resolved: parent, and whether the page failed the ancestry
// check. It asks nothing that writes.
//
// It follows the two places a real run moves a page: ResolvePage, for a page
// found by title that failed the ancestry check, and the caller, for any page
// not under all of its declared parents -- each moving it only if its direct
// parent is not parent already. A dry run creates no missing parent, so parent
// is then the deepest one that exists; a page cannot sit under a parent that
// does not exist yet, so it would be moved once a real run has created it.
func WouldMove(
	api *confluence.API,
	pg, parent *confluence.PageInfo,
	parents []string,
	misplaced bool,
) bool {
	if pg == nil || parent == nil {
		return false
	}

	if parent.Type == "folder-parent" {
		if parent.ID == dryRunFolderID {
			return true
		}

		// Not knowing means a move, as it does for EnsurePageUnderFolderParent.
		current, _, err := currentParentID(api, pg, parent.ID)
		if err != nil || current == "" {
			current = ImmediateParentID(pg)
		}

		return current != parent.ID
	}

	if !misplaced && UnderDeclaredParents(pg, parents) {
		return false
	}

	if len(parents) > 0 && !strings.EqualFold(parent.Title, parents[len(parents)-1]) {
		return true
	}

	return ImmediateParentID(pg) != parent.ID
}

// EnsurePageUnderFolderParent is EnsurePageUnderParent with folderID as the
// parent. Confluence moves a page under a folder the same way it moves one
// under a page; what differs is telling whether it is there already.
//
// A page read through v1 cannot show a folder parent -- folders are never among
// its ancestors -- so it always looked misplaced, and every run appended it to
// its folder again, reshuffling the folder's children. Before moving such a
// page, its parent is asked of v2, which names folders. That is a read in place
// of the move and the re-read that follows it, and only for a page v1 read.
func EnsurePageUnderFolderParent(
	api *confluence.API,
	pg *confluence.PageInfo,
	folderID string,
) error {
	if pg != nil && folderID != "" {
		parentID, parentType, err := currentParentID(api, pg, folderID)
		if err != nil {
			// Not knowing costs a move that may not have been needed, which is
			// what happened every time before this was asked at all.
			log.Debug().Err(err).Msgf("unable to tell whether page %q is in folder %s already", pg.Title, folderID)
		} else {
			pg.ParentID, pg.ParentType = parentID, parentType
		}
	}

	return EnsurePageUnderParent(api, pg, folderID)
}

// currentParentID returns the id and type of pg's direct parent as far as
// telling whether it is in folderID goes. A page read through v1 cannot show a
// folder parent, so when pg has no parent id and its ancestors do not end at
// folderID, v2 is asked, and its answer or error returned; otherwise it is
// pg's own ParentID and ParentType.
func currentParentID(
	api *confluence.API,
	pg *confluence.PageInfo,
	folderID string,
) (string, string, error) {
	if pg.ParentID != "" || ImmediateParentID(pg) == folderID {
		return pg.ParentID, pg.ParentType, nil
	}

	return api.ParentOfV2(pg.ID)
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
