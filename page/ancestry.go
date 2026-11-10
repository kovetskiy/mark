package page

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// ParentInfo represents either a page or folder parent
type ParentInfo struct {
	ID    string
	Title string
	Type  confluence.ContentType // confluence.ContentPage or ContentFolder
}

// FolderCache remembers the folders a run has created or found, so that files
// sharing a folder ancestry do not each resolve it again.
//
// It is keyed by space, parent and title, with no notion of which Confluence
// it came from, so one cache serves one run: a second run, against another
// instance even, would resolve folders to ids the first saw, and a folder
// renamed or deleted in between would never be looked up again.
//
// Safe for concurrent use, because an unsynchronised map write is a hard
// runtime throw rather than a wrong answer. A nil cache remembers nothing.
type FolderCache struct {
	mu  sync.RWMutex
	ids map[string]string
}

// NewFolderCache returns an empty cache, for one run. The zero value is one
// too.
func NewFolderCache() *FolderCache {
	return &FolderCache{}
}

func folderCacheKey(space, contextID, title string) string {
	return space + "\x00" + contextID + "\x00" + title
}

func (c *FolderCache) store(space, contextID, title, id string) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ids == nil {
		c.ids = map[string]string{}
	}
	c.ids[folderCacheKey(space, contextID, title)] = id
}

func (c *FolderCache) lookup(space, contextID, title string) (string, bool) {
	if c == nil {
		return "", false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.ids[folderCacheKey(space, contextID, title)]
	return id, ok
}

// resolveFolder finds the folder a title names, and may move one that an
// earlier sync left at the space root.
//
// It takes dryRun because that move is a write, and it happens while the
// ancestry is still being worked out -- long before the guard that decides
// whether this run writes anything. A run that promises to write nothing was
// reparenting a folder, and every page inside it with it.
func resolveFolder(
	ctx context.Context,
	api *confluence.API,
	dryRun bool,
	space, title, underID string,
	anchorPageID *string,
) (*confluence.FolderInfo, error) {
	underAnchor := underID != "" && anchorPageID != nil && underID == *anchorPageID

	var folder *confluence.FolderInfo
	var err error
	switch {
	case underID == "":
		folder, err = api.FindRootFolder(ctx, space, title)
	case underAnchor:
		folder, err = api.FindChildFolder(ctx, underID, confluence.ContentPage, title)
	default:
		// Anything else beneath which a folder is looked for is a folder
		// this resolution has already walked through.
		folder, err = api.FindChildFolder(ctx, underID, confluence.ContentFolder, title)
	}
	if err != nil || folder != nil {
		return folder, err
	}

	// Top-level wiki folder may exist at space root from an earlier sync; move it under MARK_PARENTS.
	if underAnchor {
		folder, err = api.FindRootFolder(ctx, space, title)
		if err != nil || folder == nil {
			return folder, err
		}
		if folder.ParentID != *anchorPageID {
			if dryRun {
				// Reported rather than done, and the folder is given back as it
				// actually is: saying where it would end up is the dry run's
				// job, and moving it there is not.
				log.Info().Msgf(
					"folder %q would be moved under the page named by --parents", title,
				)

				return folder, nil
			}

			if err := api.MoveContentAppend(ctx, folder.ID, *anchorPageID); err != nil {
				return nil, fmt.Errorf("move folder %q under MARK_PARENTS page: %w", title, err)
			}
			return api.GetFolderByID(ctx, folder.ID)
		}
	}

	return nil, nil
}

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
	RecordFolder(ctx context.Context, space, folderPath, folderID string) error
	LookupFolder(ctx context.Context, space, folderPath string) (string, bool, error)
}

// ParentTracker remembers which Confluence page a declared parent chain
// resolved to.
//
// Parents are found by title too, so renaming one leaves every document that
// declares it pointing at a name nothing carries: mark creates an empty page
// under the old title and the real one's children end up beneath it.
// Remembering what the chain resolved to makes the rename survivable for any
// parent -- including the ones no document in the repository publishes, which
// is most of them.
//
// An interface for the reason FolderTracker is one, and a nil tracker disables
// it just the same.
type ParentTracker interface {
	RecordParent(ctx context.Context, space, parentPath, pageID string) error
	LookupParent(ctx context.Context, space, parentPath string) (string, bool, error)
}

// AncestryTracker is everything the resolver can remember about where a
// document was put.
type AncestryTracker interface {
	FolderTracker
	ParentTracker
}

// AncestryOptions are the settings shared by the functions that resolve and
// create a page's ancestry.
type AncestryOptions struct {
	// DryRun looks up what exists and creates nothing.
	DryRun bool
	// FolderCache holds the folders the run has already resolved; nil
	// resolves every one.
	FolderCache *FolderCache
	// Parents and Folders remember what parent chains and folder paths
	// resolved to; nil remembers nothing. An AncestryTracker serves as both.
	Parents ParentTracker
	Folders FolderTracker
	// Warnings collects what is worth saying about where the document's page
	// was found; nil only logs it.
	Warnings *report.Warnings
}

// ParentPathKey names a parent by the chain of titles leading to it.
//
// No anchor, unlike folderPathKey: a parent chain is resolved from the space
// root, and the mapping is already per space, so the titles alone identify it.
//
// Exported because the chain is recorded here and consulted where a stale
// parent title is repaired, which is a decision made before resolution starts.
func ParentPathKey(chain []string) string {
	return strings.Join(chain, "\x00")
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

// EnsureFolderAncestry creates the folder hierarchy and returns the final parent for page creation.
// Top-level folders are created under anchorPageID (MARK_PARENTS page); nested folders nest under prior folders.
//
// It reads options.DryRun, FolderCache and Folders.
func EnsureFolderAncestry(
	ctx context.Context,
	api *confluence.API,
	space string,
	folders []string,
	anchorPageID *string,
	options AncestryOptions,
) (*ParentInfo, error) {
	if len(folders) == 0 {
		return nil, nil
	}

	// Get space ID for folder API calls
	spaceID, err := api.GetSpaceID(ctx, space)
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

		if id, ok := options.FolderCache.lookup(space, underID, title); ok {
			folder, err = api.GetFolderByID(ctx, id)
		} else {
			folder, err = resolveFolder(ctx, api, options.DryRun, space, title, underID, anchorPageID)
		}
		if err != nil {
			return nil, fmt.Errorf("error finding folder with title %q: %w", title, err)
		}

		if folder == nil && options.Folders != nil {
			// No folder carries this title. It may have been renamed in
			// Confluence since the last run, in which case the folder recorded
			// for this position is still the right one and creating another
			// would split the hierarchy in two.
			key := folderPathKey(anchorPageID, folders, i)
			id, ok, lookupErr := options.Folders.LookupFolder(ctx, space, key)
			if lookupErr != nil {
				return nil, fmt.Errorf("unable to check whether folder %q was renamed: %w", title, lookupErr)
			}
			if ok {
				folder, err = api.GetFolderByID(ctx, id)
				if err != nil {
					// Not "gone": GetFolderByID answers a 404 with (nil, nil),
					// so an error here is a 401, a 403 or a 5xx that outlived
					// every retry -- most often a scoped token without folder
					// read. Taking that for absence created a second folder
					// with the same title and split the hierarchy this lookup
					// exists to hold together, and the run that did it looked
					// like it had worked.
					return nil, fmt.Errorf(
						"unable to check whether folder %q was renamed: %w", title, err,
					)
				}

				if folder == nil {
					// Recorded, and really not there any more. Fall through and
					// create it again.
					options.Warnings.Add(report.WarningMissingPage,
						"folder %q was recorded as %s, which no longer exists; it will be created again", title, id)
				} else {
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

		if options.Folders != nil {
			if err := options.Folders.RecordFolder(ctx, space, folderPathKey(anchorPageID, folders, i), folder.ID); err != nil {
				return nil, err
			}
		}

		options.FolderCache.store(space, underID, title, folder.ID)
		log.Debug().Msgf("folder %q exists: %s", title, folder.ID)

		rest = folders[i:]
		parent = &ParentInfo{
			ID:    folder.ID,
			Title: folder.Title,
			Type:  confluence.ContentFolder,
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

	if !options.DryRun {
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
				folder, err = api.CreateFolder(ctx, spaceID, title, anchorPageID, confluence.ContentPage)
			} else {
				pid := parent.ID
				folder, err = api.CreateFolder(ctx, spaceID, title, &pid, confluence.ContentFolder)
			}
			if err != nil {
				createErr := err

				underID := ""
				if parent != nil {
					underID = parent.ID
				} else if anchorPageID != nil {
					underID = *anchorPageID
				}

				// Another file in the same run may have created this folder
				// already. Asked by looking for the folder rather than by
				// reading the refusal: the "folder exists with the same title"
				// wording comes from Confluence and is not a contract, so a
				// reworded or localised instance turned a collision anybody can
				// hit into a hard failure. A create that failed for a real
				// reason leaves nothing to find, and the original error is what
				// gets reported.
				if id, ok := options.FolderCache.lookup(space, underID, title); ok {
					folder, err = api.GetFolderByID(ctx, id)
				} else {
					folder, err = resolveFolder(ctx, api, options.DryRun, space, title, underID, anchorPageID)
				}
				if err != nil || folder == nil {
					return nil, fmt.Errorf(
						"error creating folder with title %q: %w",
						title,
						createErr,
					)
				}

				log.Info().Msgf(
					"folder %q already exists as %s; using it rather than creating another",
					title, folder.ID,
				)
			}

			underID := ""
			if parent != nil {
				underID = parent.ID
			} else if anchorPageID != nil {
				underID = *anchorPageID
			}
			options.FolderCache.store(space, underID, title, folder.ID)

			if options.Folders != nil {
				key := folderPathKey(anchorPageID, folders, firstNew+offset)
				if err := options.Folders.RecordFolder(ctx, space, key, folder.ID); err != nil {
					return nil, err
				}
			}

			parent = &ParentInfo{
				ID:    folder.ID,
				Title: folder.Title,
				Type:  confluence.ContentFolder,
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
				ID:    dryRunFolderID,
				Title: finalTitle,
				Type:  confluence.ContentFolder,
			}
		}
	}

	return parent, nil
}

// EnsureMixedAncestry creates folders under the MARK_PARENTS anchor page, then returns a folder-parent
// marker so leaf pages are created inside the deepest folder.
func EnsureMixedAncestry(
	ctx context.Context,
	api *confluence.API,
	space string,
	folders []string,
	pages []string,
	options AncestryOptions,
) (*confluence.PageInfo, error) {
	var anchorPageID *string

	if len(pages) > 0 {
		anchor, err := EnsureAncestry(ctx, api, space, pages, options)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve MARK_PARENTS page ancestry: %w", err)
		}
		if anchor == nil {
			return nil, fmt.Errorf("MARK_PARENTS page chain %q could not be resolved", strings.Join(pages, " > "))
		}
		anchorPageID = &anchor.ID

		// A dry run creates no missing page, so the anchor is then the deepest
		// one that exists, and the folders found under it are where the page
		// sits now rather than where a real run would put it. A real run
		// creates the rest of the chain and new folders beneath it, so the
		// folder parent is one that does not exist yet.
		if options.DryRun && len(folders) > 0 && !strings.EqualFold(anchor.Title, pages[len(pages)-1]) {
			log.Info().Msgf(
				"skipping folder creation due to dry-run mode: MARK_PARENTS chain %q is not there yet, so neither are folders %v",
				strings.Join(pages, " > "), folders,
			)

			return &confluence.PageInfo{
				ID:    dryRunFolderID,
				Type:  "folder-parent",
				Title: folders[len(folders)-1],
			}, nil
		}
	}

	if len(folders) == 0 {
		if len(pages) == 0 {
			return nil, nil
		}
		return EnsureAncestry(ctx, api, space, pages, options)
	}

	folderParent, err := EnsureFolderAncestry(ctx, api, space, folders, anchorPageID, options)
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

// EnsureAncestry finds the chain of parent pages titled ancestry, creating the
// missing ones, and returns the last. It reads options.DryRun and Parents.
func EnsureAncestry(
	ctx context.Context,
	api *confluence.API,
	space string,
	ancestry []string,
	options AncestryOptions,
) (*confluence.PageInfo, error) {
	var parent *confluence.PageInfo

	rest := ancestry

	for i, title := range ancestry {
		page, err := api.FindPage(ctx, space, title, confluence.ContentPage)
		if err != nil {
			return nil, fmt.Errorf("error during finding parent page with title %q: %w", title, err)
		}

		if page == nil {
			break
		}

		log.Debug().Msgf("parent page %q exists: %s", title, page.Links.Full)

		if options.Parents != nil {
			if err := options.Parents.RecordParent(ctx, space, ParentPathKey(ancestry[:i+1]), page.ID); err != nil {
				return nil, err
			}
		}

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.FindRootPage(ctx, space)
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

	if !options.DryRun {
		// rest is the tail of ancestry that does not exist yet, so its offset
		// within ancestry is what makes a recorded key match on the next run.
		firstNew := len(ancestry) - len(rest)
		for offset, title := range rest {
			page, err := api.CreatePage(ctx, space, confluence.ContentPage, parent, title, ``)
			if err != nil {
				return nil, fmt.Errorf("error during creating parent page with title %q: %w", title, err)
			}

			if options.Parents != nil {
				key := ParentPathKey(ancestry[:firstNew+offset+1])
				if err := options.Parents.RecordParent(ctx, space, key, page.ID); err != nil {
					return nil, err
				}
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
	ctx context.Context,
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(ctx, space, ancestry[len(ancestry)-1], confluence.ContentPage)
	if err != nil {
		return nil, err
	}

	if page == nil {
		return nil, nil
	}

	isHomepage := false
	if len(page.Ancestors) < 1 {
		// Compared against, not used, so a space that will not give up its home
		// page must not fail every document in it. Without one to compare
		// against, a parentless page is treated as the misplacement below --
		// which is recoverable, where refusing outright was not.
		homepage, err := api.FindHomePage(ctx, space)
		if err != nil {
			log.Debug().Err(err).Msgf(
				"no home page for space %q; cannot tell whether %q is it",
				space, page.Title,
			)

			homepage = nil
		}

		if homepage != nil && page.ID == homepage.ID {
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

		// The page's own title is not compared: it was just found by
		// ancestry[len(ancestry)-1], so it is the title asked for.
		valid := len(actual) == len(ancestry)-1
		for i := 0; valid && i < len(actual); i++ {
			valid = strings.EqualFold(actual[i], ancestry[i])
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
		if !hasAncestorTitled(page, parent) {
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
