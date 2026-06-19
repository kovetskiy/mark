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

// EnsurePageUnderFolderParent moves an existing page under folderID when its direct parent differs.
func EnsurePageUnderFolderParent(
	api *confluence.API,
	pg *confluence.PageInfo,
	folderID string,
) error {
	if pg == nil || folderID == "" {
		return nil
	}

	currentParent := ImmediateParentID(pg)
	if currentParent == folderID {
		return nil
	}

	log.Info().Msgf(
		"moving page %q (%s) from parent %s under folder %s",
		pg.Title,
		pg.ID,
		currentParent,
		folderID,
	)

	if err := api.MoveContentAppend(pg.ID, folderID); err != nil {
		return fmt.Errorf("move page %q under folder: %w", pg.Title, err)
	}

	refreshed, err := api.GetPageByID(pg.ID)
	if err != nil {
		return fmt.Errorf("refresh page %q after move: %w", pg.Title, err)
	}
	if refreshed == nil {
		return fmt.Errorf("page %q not found after move", pg.Title)
	}

	*pg = *refreshed
	return nil
}
