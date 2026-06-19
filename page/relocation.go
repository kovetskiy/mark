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
