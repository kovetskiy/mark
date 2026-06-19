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

// EnsureFolderAncestry creates the folder hierarchy and returns the final parent for page creation
func EnsureFolderAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	folders []string,
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
		folder, err := api.FindFolder(space, title)
		if err != nil {
			return nil, fmt.Errorf("error finding folder with title %q: %w", title, err)
		}

		if folder == nil {
			break
		}

		// Verify this folder is at the expected level of hierarchy
		if i == 0 {
			// The root folder in our list should not have a folder parent
			if folder.ParentType == "folder" && folder.ParentID != "" {
				break
			}
		} else {
			// Subsequent folders must have the previous folder as parent
			if folder.ParentID != parent.ID || folder.ParentType != "folder" {
				break
			}
		}

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
		var parentID *string
		if parent != nil {
			parentID = &parent.ID
		}

		for _, title := range rest {
			folder, err := api.CreateFolder(spaceID, title, parentID)
			if err != nil {
				// Another file in the same run may have created this folder already.
				if strings.Contains(err.Error(), "folder exists with the same title") {
					folder, err = api.FindFolder(space, title)
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

			parent = &ParentInfo{
				ID:    folder.ID,
				Title: folder.Title,
				Type:  "folder",
			}
			parentID = &parent.ID
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

// EnsureMixedAncestry creates both folders and pages in the correct order
func EnsureMixedAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	folders []string,
	pages []string,
) (*confluence.PageInfo, error) {
	// First, create the folder hierarchy
	folderParent, err := EnsureFolderAncestry(dryRun, api, space, folders)
	if err != nil {
		return nil, fmt.Errorf("failed to create folder hierarchy: %w", err)
	}

	// If we have no page hierarchy, the target page will be created directly under the folder
	if len(pages) == 0 {
		// We need to create a special page that represents the folder parent
		// This is a bit of a hack, but we'll use the folder ID as a special marker
		if folderParent != nil {
			// Create a fake PageInfo that contains the folder ID in a special field
			// The calling code can detect this and use CreatePageWithFolderParent
			return &confluence.PageInfo{
				ID:    folderParent.ID, // Use folder ID
				Type:  "folder-parent", // Special marker
				Title: folderParent.Title,
			}, nil
		}
		return nil, nil
	}

	// Now handle page ancestry, starting from the folder parent
	var pageParent *confluence.PageInfo

	if folderParent != nil {
		// Find existing pages under the folder parent
		rest := pages
		for i, title := range pages {
			page, err := api.FindPage(space, title, "page")
			if err != nil {
				return nil, fmt.Errorf("error finding page %q: %w", title, err)
			}

			if page == nil {
				break
			}

			// Verify this page is actually under our folder parent
			// (we could add validation here if needed)
			log.Debug().Msgf("page %q exists under folder hierarchy", title)
			rest = pages[i:]
			pageParent = page
		}

		if pageParent != nil {
			rest = rest[1:]
		}

		// Create remaining pages
		if len(rest) > 0 && !dryRun {
			if pageParent == nil {
				// Create first page under folder
				page, err := api.CreatePageWithFolderParent(space, "page", folderParent.ID, rest[0], "")
				if err != nil {
					return nil, fmt.Errorf("error creating page %q under folder: %w", rest[0], err)
				}
				pageParent = page
				rest = rest[1:]
			}

			// Create remaining pages in hierarchy
			for _, title := range rest {
				page, err := api.CreatePage(space, "page", pageParent, title, "")
				if err != nil {
					return nil, fmt.Errorf("error creating page %q: %w", title, err)
				}
				pageParent = page
			}
		} else if len(rest) > 0 && dryRun {
			log.Info().Msgf("skipping page creation due to dry-run mode, need to create %d pages: %v", len(rest), rest)
		}
	} else {
		// No folders, use standard page ancestry
		pageParent, err = EnsureAncestry(dryRun, api, space, pages)
		if err != nil {
			return nil, err
		}
	}

	return pageParent, nil
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
