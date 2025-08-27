package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/confluence"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
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
		return nil, karma.Format(err, "failed to get space ID for %q", space)
	}

	var parent *ParentInfo
	rest := folders

	// Find existing folders from the beginning of the hierarchy
	for i, title := range folders {
		folder, err := api.FindFolder(space, title)
		if err != nil {
			return nil, karma.Format(
				err,
				"error finding folder with title %q",
				title,
			)
		}

		if folder == nil {
			break
		}

		log.Debugf(nil, "folder %q exists: %s", title, folder.ID)

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

	log.Debugf(
		nil,
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
				return nil, karma.Format(
					err,
					"error creating folder with title %q",
					title,
				)
			}

			parent = &ParentInfo{
				ID:    folder.ID,
				Title: folder.Title,
				Type:  "folder",
			}
			parentID = &parent.ID
		}
	} else {
		log.Infof(
			nil,
			"skipping folder creation due to dry-run mode, "+
				"need to create %d folders: %v",
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
		return nil, karma.Format(err, "failed to create folder hierarchy")
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
				return nil, karma.Format(err, "error finding page %q", title)
			}

			if page == nil {
				break
			}

			// Verify this page is actually under our folder parent
			// (we could add validation here if needed)
			log.Debugf(nil, "page %q exists under folder hierarchy", title)
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
					return nil, karma.Format(err, "error creating page %q under folder", rest[0])
				}
				pageParent = page
				rest = rest[1:]
			}

			// Create remaining pages in hierarchy
			for _, title := range rest {
				page, err := api.CreatePage(space, "page", pageParent, title, "")
				if err != nil {
					return nil, karma.Format(err, "error creating page %q", title)
				}
				pageParent = page
			}
		} else if len(rest) > 0 && dryRun {
			log.Infof(nil, "skipping page creation due to dry-run mode, need to create %d pages: %v", len(rest), rest)
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
			return nil, karma.Format(
				err,
				`error during finding parent page with title %q`,
				title,
			)
		}

		if page == nil {
			break
		}

		log.Debugf(nil, "parent page %q exists: %s", title, page.Links.Full)

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.FindRootPage(space)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't find root page for space %q",
				space,
			)
		}

		parent = page
	}
	if len(rest) == 0 {
		return parent, nil
	}

	log.Debugf(
		nil,
		"empty pages under %q to be created: %s",
		parent.Title,
		strings.Join(rest, ` > `),
	)

	if !dryRun {
		for _, title := range rest {
			page, err := api.CreatePage(space, "page", parent, title, ``)
			if err != nil {
				return nil, karma.Format(
					err,
					`error during creating parent page with title %q`,
					title,
				)
			}

			parent = page
		}
	} else {
		log.Infof(
			nil,
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
			return nil, karma.Format(
				err,
				"can't obtain home page from space %q",
				space,
			)
		}

		if page.ID == homepage.ID {
			log.Debugf(nil, "page is homepage for space %q", space)
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
			return nil, karma.Describe("title", page.Title).
				Describe("actual", strings.Join(actual, " > ")).
				Describe("expected", strings.Join(ancestry, " > ")).
				Format(nil, "the page has fewer parents than expected")
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

			return nil, karma.Describe("expected parent", parent).
				Describe("list", strings.Join(list, "; ")).
				Format(
					nil,
					"unexpected ancestry tree, did not find expected parent page in the tree",
				)
		}
	}

	return page, nil
}
