package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// ParentInfo represents either a page or folder parent
type ParentInfo struct {
	ID    string
	Title string
	Type  string // "page" or "folder"
}

// ponytail: process-wide folder cache; mark syncs files sequentially so no lock needed.
var createdFolderCache = map[string]string{}

func folderCacheKey(space, contextID, title string) string {
	return space + "\x00" + contextID + "\x00" + title
}

func cacheFolder(space, contextID, title, id string) {
	createdFolderCache[folderCacheKey(space, contextID, title)] = id
}

func cachedFolderID(space, contextID, title string) (string, bool) {
	id, ok := createdFolderCache[folderCacheKey(space, contextID, title)]
	return id, ok
}

func resolveFolder(
	api *confluence.API,
	space, title, underID string,
	anchorPageID *string,
) (*confluence.FolderInfo, error) {
	folder, err := api.FindFolder(space, title, underID)
	if err != nil {
		return nil, err
	}
	if folder != nil {
		return folder, nil
	}

	// Top-level wiki folder may exist at space root from an earlier sync; move it under MARK_PARENTS.
	if underID != "" && anchorPageID != nil && underID == *anchorPageID {
		folder, err = api.FindFolder(space, title, "")
		if err != nil || folder == nil {
			return folder, err
		}
		if folder.ParentID != *anchorPageID {
			if err := api.MoveContentAppend(folder.ID, *anchorPageID); err != nil {
				return nil, fmt.Errorf("move folder %q under MARK_PARENTS page: %w", title, err)
			}
			return api.GetFolderByID(folder.ID)
		}
	}

	return nil, nil
}

// EnsureFolderAncestry creates the folder hierarchy and returns the final parent for page creation.
// Top-level folders are created under anchorPageID (MARK_PARENTS page); nested folders nest under prior folders.
func EnsureFolderAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	folders []string,
	anchorPageID *string,
) (*ParentInfo, error) {
	if len(folders) == 0 {
		return nil, nil
	}

	// Get space ID for folder API calls
	spaceID, err := api.GetSpaceID(space)
	if err != nil {
		return nil, fmt.Errorf("failed to get space ID for %q: %w", space, err)
	}

	var parent *ParentInfo
	rest := folders

	// Find existing folders from the beginning of the hierarchy
	for i, title := range folders {
		var folder *confluence.FolderInfo
		var err error

		underID := ""
		if parent != nil {
			underID = parent.ID
		} else if anchorPageID != nil {
			underID = *anchorPageID
		}

		if id, ok := cachedFolderID(space, underID, title); ok {
			folder, err = api.GetFolderByID(id)
		} else {
			folder, err = resolveFolder(api, space, title, underID, anchorPageID)
		}
		if err != nil {
			return nil, fmt.Errorf("error finding folder with title %q: %w", title, err)
		}

		if folder == nil {
			break
		}

		cacheFolder(space, underID, title, folder.ID)
		log.Debug().Msgf("folder %q exists: %s", title, folder.ID)

		rest = folders[i:]
		parent = &ParentInfo{
			ID:    folder.ID,
			Title: folder.Title,
			Type:  "folder",
		}
	}

	if parent != nil {
		rest = rest[1:]
	}

	if len(rest) == 0 {
		return parent, nil
	}

	log.Debug().Msgf(
		"folders to be created: %s",
		strings.Join(rest, ` > `),
	)

	if !dryRun {
		for _, title := range rest {
			var folder *confluence.FolderInfo
			var err error

			if parent == nil {
				if anchorPageID == nil {
					return nil, fmt.Errorf(
						"cannot create top-level folder %q without a MARK_PARENTS anchor page",
						title,
					)
				}
				folder, err = api.CreateFolder(spaceID, title, anchorPageID, "page")
			} else {
				pid := parent.ID
				folder, err = api.CreateFolder(spaceID, title, &pid, "folder")
			}
			if err != nil {
				underID := ""
				if parent != nil {
					underID = parent.ID
				} else if anchorPageID != nil {
					underID = *anchorPageID
				}
				// Another file in the same run may have created this folder already.
				if strings.Contains(err.Error(), "folder exists with the same title") {
					if id, ok := cachedFolderID(space, underID, title); ok {
						folder, err = api.GetFolderByID(id)
					} else {
						folder, err = resolveFolder(api, space, title, underID, anchorPageID)
					}
				}
				if err != nil {
					return nil, fmt.Errorf(
						"error creating folder with title %q: %w",
						title,
						err,
					)
				}
				if folder == nil {
					return nil, fmt.Errorf(
						"folder %q reported as existing but could not be found in space %q",
						title,
						space,
					)
				}
			}

			underID := ""
			if parent != nil {
				underID = parent.ID
			} else if anchorPageID != nil {
				underID = *anchorPageID
			}
			cacheFolder(space, underID, title, folder.ID)

			parent = &ParentInfo{
				ID:    folder.ID,
				Title: folder.Title,
				Type:  "folder",
			}
		}
	} else {
		log.Info().Msgf(
			"skipping folder creation due to dry-run mode, need to create %d folders: %v",
			len(rest),
			rest,
		)
		// For dry-run, simulate the final parent
		if len(rest) > 0 {
			finalTitle := rest[len(rest)-1]
			parent = &ParentInfo{
				ID:    "dry-run-folder-id",
				Title: finalTitle,
				Type:  "folder",
			}
		}
	}

	return parent, nil
}

// EnsureMixedAncestry creates folders under the MARK_PARENTS anchor page, then returns a folder-parent
// marker so leaf pages are created inside the deepest folder.
func EnsureMixedAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	folders []string,
	pages []string,
) (*confluence.PageInfo, error) {
	var anchorPageID *string

	if len(pages) > 0 {
		anchor, err := EnsureAncestry(dryRun, api, space, pages)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve MARK_PARENTS page ancestry: %w", err)
		}
		if anchor == nil {
			return nil, fmt.Errorf("MARK_PARENTS page chain %q could not be resolved", strings.Join(pages, " > "))
		}
		anchorPageID = &anchor.ID
	}

	if len(folders) == 0 {
		if len(pages) == 0 {
			return nil, nil
		}
		return EnsureAncestry(dryRun, api, space, pages)
	}

	folderParent, err := EnsureFolderAncestry(dryRun, api, space, folders, anchorPageID)
	if err != nil {
		return nil, fmt.Errorf("failed to create folder hierarchy: %w", err)
	}

	if folderParent == nil {
		if anchorPageID != nil {
			return &confluence.PageInfo{ID: *anchorPageID, Title: pages[len(pages)-1]}, nil
		}
		return nil, nil
	}

	return &confluence.PageInfo{
		ID:    folderParent.ID,
		Type:  "folder-parent",
		Title: folderParent.Title,
	}, nil
}

func EnsureAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	var parent *confluence.PageInfo

	rest := ancestry

	for i, title := range ancestry {
		page, err := api.FindPage(space, title, "page")
		if err != nil {
			return nil, fmt.Errorf("error during finding parent page with title %q: %w", title, err)
		}

		if page == nil {
			break
		}

		log.Debug().Msgf("parent page %q exists: %s", title, page.Links.Full)

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.FindRootPage(space)
		if err != nil {
			return nil, fmt.Errorf("can't find root page for space %q: %w", space, err)
		}

		parent = page
	}
	if len(rest) == 0 {
		return parent, nil
	}

	log.Debug().
		Msgf(
			"empty pages under %q to be created: %s",
			parent.Title,
			strings.Join(rest, ` > `),
		)

	if !dryRun {
		for _, title := range rest {
			page, err := api.CreatePage(space, "page", parent, title, ``)
			if err != nil {
				return nil, fmt.Errorf("error during creating parent page with title %q: %w", title, err)
			}

			parent = page
		}
	} else {
		log.Info().
			Msgf(
				"skipping page creation due to enabled dry-run mode, "+
					"need to create %d pages: %v",
				len(rest),
				rest,
			)
	}

	return parent, nil
}

func ValidateAncestry(
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(space, ancestry[len(ancestry)-1], "page")
	if err != nil {
		return nil, err
	}

	if page == nil {
		return nil, nil
	}

	isHomepage := false
	if len(page.Ancestors) < 1 {
		homepage, err := api.FindHomePage(space)
		if err != nil {
			return nil, fmt.Errorf("can't obtain home page from space %q: %w", space, err)
		}

		if page.ID == homepage.ID {
			log.Debug().Msgf("page is homepage for space %q", space)
			isHomepage = true
		} else {
			return nil, fmt.Errorf(`page %q has no parents`, page.Title)
		}
	}

	if !isHomepage && len(page.Ancestors) < len(ancestry) {
		actual := []string{}
		for _, ancestor := range page.Ancestors {
			actual = append(actual, ancestor.Title)
		}

		valid := false

		if len(actual) == len(ancestry)-1 {
			broken := false
			for i := 0; i < len(actual); i++ {
				if actual[i] != ancestry[i] {
					broken = true
					break
				}
			}

			if !broken {
				if ancestry[len(ancestry)-1] == page.Title {
					valid = true
				}
			}
		}

		if !valid {
			return nil, fmt.Errorf(
				"the page has fewer parents than expected: title=%q, actual=[%s], expected=[%s]",
				page.Title, strings.Join(actual, " > "), strings.Join(ancestry, " > "),
			)
		}
	}

	for _, parent := range ancestry[:len(ancestry)-1] {
		found := false

		// skipping root article title
		for _, ancestor := range page.Ancestors {
			if ancestor.Title == parent {
				found = true
				break
			}
		}

		if !found {
			list := []string{}

			for _, ancestor := range page.Ancestors {
				list = append(list, ancestor.Title)
			}

			return nil, fmt.Errorf(
				"unexpected ancestry tree, did not find expected parent page %q in the tree: actual=[%s]",
				parent, strings.Join(list, "; "),
			)
		}
	}

	return page, nil
}
