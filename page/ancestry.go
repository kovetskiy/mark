package page

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// ParentInfo represents either a page or folder parent
type ParentInfo struct {
	ID    string
	Title string
	Type  string // "page" or "folder"
}

// Process-wide cache of folders created or found during a run, so that files
// sharing a folder ancestry do not each re-resolve it.
//
// The mutex is not optional even though mark currently syncs files
// sequentially: the map is package state, so a caller using the library from
// more than one goroutine -- or any future move to parallel file processing --
// would otherwise corrupt it, and an unsynchronised map write is a hard
// runtime throw rather than a subtle wrong answer.
var (
	createdFolderCache = map[string]string{}
	createdFolderMutex sync.RWMutex
)

// ResetFolderCache empties the folder cache.
//
// The cache is package-level and keyed only by space, parent and title, with no
// notion of which Confluence it came from. That is fine within one run and
// wrong across two: Run is a public entry point, so a process can publish twice
// -- to different instances, even -- and the second would resolve folders to
// ids the first saw. It also means a folder renamed or deleted between runs is
// never looked up again.
//
// Run calls this before it starts, so a run never inherits another's answers.
func ResetFolderCache() {
	createdFolderMutex.Lock()
	defer createdFolderMutex.Unlock()
	clear(createdFolderCache)
}

func folderCacheKey(space, contextID, title string) string {
	return space + "\x00" + contextID + "\x00" + title
}

func cacheFolder(space, contextID, title, id string) {
	createdFolderMutex.Lock()
	defer createdFolderMutex.Unlock()
	createdFolderCache[folderCacheKey(space, contextID, title)] = id
}

func cachedFolderID(space, contextID, title string) (string, bool) {
	createdFolderMutex.RLock()
	defer createdFolderMutex.RUnlock()
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
		if underID == "" {
			if folder.ParentType == "folder" || folder.ParentType == "page" {
				return nil, nil
			}
		} else {
			if folder.ParentID != underID {
				return nil, nil
			}
		}
		return folder, nil
	}

	// Top-level wiki folder may exist at space root from an earlier sync; move it under MARK_PARENTS.
	if underID != "" && anchorPageID != nil && underID == *anchorPageID {
		folder, err = api.FindFolder(space, title, "")
		if err != nil || folder == nil {
			return folder, err
		}
		// Validate that the folder found at space root does not have any folder or page parent
		if folder.ParentType == "folder" || folder.ParentType == "page" {
			return nil, nil
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
// FolderTracker remembers which Confluence folder a declared folder path
// resolved to.
//
// Folders are found by title, and mark creates one when the title is not found.
// A folder renamed in Confluence therefore stops matching the header that
// declares it, and mark builds a second folder beside the first and moves pages
// into it, splitting the hierarchy. Remembering the folder makes the rename
// survivable.
//
// An interface rather than the concrete store so this package keeps knowing
// nothing about where the mapping is kept. A nil tracker disables the whole
// mechanism, which is what every caller that does not opt in passes.
type FolderTracker interface {
	RecordFolder(space, folderPath, folderID string) error
	LookupFolder(space, folderPath string) (string, bool, error)
}

// folderPathKey names a folder by the chain of titles leading to it, under the
// anchor it hangs from. The anchor is part of the key because the same chain of
// titles under a different anchor is a different folder.
func folderPathKey(anchorPageID *string, folders []string, upto int) string {
	anchor := ""
	if anchorPageID != nil {
		anchor = *anchorPageID
	}
	return anchor + "\x00" + strings.Join(folders[:upto+1], "\x00")
}

func EnsureFolderAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	folders []string,
	anchorPageID *string,
	tracker FolderTracker,
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

		if folder == nil && tracker != nil {
			// No folder carries this title. It may have been renamed in
			// Confluence since the last run, in which case the folder recorded
			// for this position is still the right one and creating another
			// would split the hierarchy in two.
			key := folderPathKey(anchorPageID, folders, i)
			id, ok, lookupErr := tracker.LookupFolder(space, key)
			if lookupErr != nil {
				return nil, fmt.Errorf("unable to check whether folder %q was renamed: %w", title, lookupErr)
			}
			if ok {
				folder, err = api.GetFolderByID(id)
				if err != nil {
					// Recorded but gone. Fall through and create it again.
					log.Warn().Msgf("folder %q was recorded as %s, which no longer exists", title, id)
					folder = nil
				} else if folder != nil {
					log.Info().Msgf(
						"folder %q was renamed to %q; using it rather than creating another",
						title, folder.Title,
					)
				}
			}
		}

		if folder == nil {
			break
		}

		if tracker != nil {
			if err := tracker.RecordFolder(space, folderPathKey(anchorPageID, folders, i), folder.ID); err != nil {
				return nil, err
			}
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
		// rest is the tail of folders that does not exist yet, so its offset
		// within folders is what makes a recorded key match on the next run.
		firstNew := len(folders) - len(rest)
		for offset, title := range rest {
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

			if tracker != nil {
				key := folderPathKey(anchorPageID, folders, firstNew+offset)
				if err := tracker.RecordFolder(space, key, folder.ID); err != nil {
					return nil, err
				}
			}

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
	tracker FolderTracker,
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

	folderParent, err := EnsureFolderAncestry(dryRun, api, space, folders, anchorPageID, tracker)
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

// ErrAncestryMismatch reports that a page exists but does not sit where its
// headers say it should.
//
// Distinguished from every other way ancestry resolution can fail because it is
// the one that is recoverable: the document has declared a different parent, and
// moving the page there is what it asked for. Everything else -- a page with no
// parents that is not the homepage, a chain that cannot be resolved -- is a
// genuine impasse.
var ErrAncestryMismatch = errors.New("page is not under the declared parents")

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
			// The page sits at the root of its space and is not the homepage,
			// while a document that declares no parents is placed under the
			// space root page. So the two disagree about where it belongs --
			// which is a misplacement like any other, and is reported as one so
			// the caller can move it rather than refuse.
			//
			// It used to be a flat refusal, which left the page unpublishable
			// by mark at all: nothing mark could be told would move it, because
			// declaring the parent it ought to have is precisely what produces
			// this state.
			return page, fmt.Errorf(
				"%w: %q sits at the root of the space", ErrAncestryMismatch, page.Title,
			)
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
			return page, fmt.Errorf(
				"%w: title=%q, actual=[%s], expected=[%s]",
				ErrAncestryMismatch,
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

			return page, fmt.Errorf(
				"%w: expected parent %q, actual=[%s]",
				ErrAncestryMismatch,
				parent, strings.Join(list, "; "),
			)
		}
	}

	return page, nil
}
