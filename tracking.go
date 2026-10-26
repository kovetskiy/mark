// Resolving and recording pages through the --track-pages record.

package mark

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/rs/zerolog/log"
)

// previewTrackedResolution says what a real run would have done with a document
// the title lookup could not place.
//
// A dry run that reports a new page for every rename and retitle the real run
// would have handled in place is worse than no dry run, because it is confidently
// wrong about the one thing somebody is checking.
func previewTrackedResolution(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
	sourceHash string,
) (*confluence.PageInfo, error) {
	if tracker == nil || meta == nil {
		return nil, nil
	}

	// Errors stop the preview as they stop a real run: reporting "would create"
	// over a lookup that failed would hide the duplicate a real run refuses to make.
	pg, err := resolveTrackedPage(tracker, api, meta, file)
	if err != nil {
		return nil, err
	}

	if pg != nil {
		log.Info().Msgf(
			"%s would be published to the existing page %s, retitled from %q to %q",
			file, pg.ID, pg.Title, meta.Title,
		)

		return pg, nil
	}

	pg, err = resolveRenamedFile(tracker, api, meta, file, sourceHash)
	if err != nil {
		return nil, err
	}

	if pg != nil {
		log.Info().Msgf(
			"%s would be treated as a rename of an already published document, updating page %s",
			file, pg.ID,
		)

		return pg, nil
	}

	log.Info().Msgf("%s would be published as a new page %q", file, meta.Title)

	return nil, nil
}

// resolveTrackedPage returns the page this file published to on a previous run,
// for the case where looking the page up by title found nothing.
//
// That lookup failing is ambiguous: the page may be new, or it may be the same
// page under a title that has since changed -- through the Title header, the
// leading H1, or a rename feeding --title-from-filename. Without the manifest
// the two are indistinguishable and mark publishes a duplicate.
//
// Returning (nil, nil) means "no answer, carry on and create", which covers both
// a file that has genuinely never been published and one whose recorded page has
// since been deleted in Confluence. Neither is a failure worth stopping for.
func resolveTrackedPage(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
) (*confluence.PageInfo, error) {
	if tracker == nil || meta == nil {
		return nil, nil
	}

	entry, ok, err := tracker.Lookup(meta.Space, file)
	if err != nil {
		return nil, fmt.Errorf("unable to read page mapping for %q: %w", file, err)
	}
	if !ok {
		return nil, nil
	}

	pg, err := api.GetPageByID(entry.PageID)
	if err != nil {
		// Only a page that is genuinely gone may fall through to being created
		// again. Treating every failure that way would mean a network blip
		// publishes a duplicate -- precisely the outcome this exists to prevent
		// -- so anything else stops the run and is reported.
		if !errors.Is(err, confluence.ErrNotFound) {
			return nil, fmt.Errorf(
				"unable to load page %s recorded for %q: %w", entry.PageID, file, err,
			)
		}
		log.Warn().Msgf(
			"%s was published to page %s, which no longer exists; a new page will be created",
			file, entry.PageID,
		)
		return nil, nil
	}

	if pg.Title != meta.Title {
		log.Info().Msgf(
			"%s was published as %q and is now %q; retitling page %s rather than creating a second one",
			file, pg.Title, meta.Title, pg.ID,
		)
	}

	return pg, nil
}

// refreshStaleParents rewrites parent titles that no longer resolve to the
// titles those pages carry now.
//
// mark names a parent by title and creates it when the title is not found, so
// renaming a page that others declare as their parent strands them: an empty
// page appears under the old title and the real one's children are moved
// beneath it. Tracking makes that more likely rather than less, because the
// parent is now renamed in place instead of being duplicated somewhere visible.
//
// Three answers are tried, in order of how precisely each answers the question.
// A document of this run that publishes the title is the parent itself, about
// to retitle the page it published last time -- under its path, or under the
// path it had before a rename. The chain a document declares was recorded when
// it last resolved, so it names the page that held this exact position --
// including a parent nobody publishes from the repository, which is what most
// parents are. Failing that, a title mark itself published is followed, and
// only when exactly one page was published under it. Anything ambiguous, or a
// title none of them has seen, is left exactly as written -- a parent that is
// genuinely meant to be created still is.
//
// The rewrite happens here rather than inside ancestry resolution because
// everything downstream compares titles: validation, the decision that a page
// is misplaced, and the lookup that walks the chain. Repairing the title once,
// before any of them run, leaves them all agreeing.
func refreshStaleParents(tracker *manifest.Store, api *confluence.API, meta *metadata.Meta, titles *runTitles) error {
	if tracker == nil {
		return nil
	}

	// Keys name the chain as the document declares it, which is not what
	// meta.Parents holds once an earlier entry has been rewritten. A chain
	// whose first parent was also renamed would otherwise be looked up under a
	// name it was never recorded under.
	declared := slices.Clone(meta.Parents)

	for i, title := range declared {
		// FindPage is memoised, and ancestry resolution is about to ask the
		// same question, so this costs nothing on the common path where the
		// parent is exactly where it says it is.
		existing, err := api.FindPage(meta.Space, title, "page")
		if err != nil {
			// A lookup that failed is not a parent that is missing. Reading it
			// as one leaves the stale title in place, and a stale title is
			// exactly the condition this exists to prevent: ancestry creates an
			// empty page under the old name and moves the real one's children
			// beneath it. resolveTrackedPage refuses the same conflation.
			return fmt.Errorf(
				"unable to look up parent %q in space %q: %w", title, meta.Space, err,
			)
		}
		if existing != nil {
			continue
		}

		// A child sorting ahead of its retitled parent must not create the new title first.
		pageID, publisher, err := trackedPageOfDeclaredTitle(tracker, titles, meta.Space, title)
		if err != nil {
			return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
		}
		ok := pageID != ""
		if !ok {
			pageID, ok, err = tracker.LookupParent(meta.Space, page.ParentPathKey(declared[:i+1]))
			if err != nil {
				return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
			}
		}
		if !ok {
			pageID, ok, err = tracker.ResolveStaleTitle(meta.Space, title)
			if err != nil {
				return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
			}
			if !ok {
				continue
			}
		}

		renamed, err := api.GetPageByID(pageID)
		if err != nil {
			// Only a recorded page that is genuinely gone may be passed over.
			// Anything else is a failure to read, and carrying on with the old
			// title turns a network blip into a duplicate parent.
			if !errors.Is(err, confluence.ErrNotFound) {
				return fmt.Errorf(
					"unable to load page %s recorded for parent %q: %w", pageID, title, err,
				)
			}

			log.Warn().Msgf(
				"parent %q was recorded as page %s, which no longer exists; leaving the title as written",
				title, pageID,
			)

			continue
		}
		if renamed == nil || renamed.Title == title {
			continue
		}

		if publisher != "" {
			log.Info().Msgf(
				"parent %q is page %s, titled %q until %s retitles it; using it rather than creating an empty page",
				title, pageID, renamed.Title, publisher,
			)
		} else {
			log.Info().Msgf(
				"parent %q was renamed to %q; using it rather than creating a page under the old title",
				title, renamed.Title,
			)
		}
		meta.Parents[i] = renamed.Title
	}

	return nil
}

// trackedPageOfDeclaredTitle returns the page recorded for the one document of
// the run that publishes the title as a page, and that document, if it has been
// published before. A document renamed since is found by its content, as
// resolveRenamedFile finds it when its own turn comes.
func trackedPageOfDeclaredTitle(tracker *manifest.Store, titles *runTitles, space, title string) (string, string, error) {
	file, ok := titles.declaredBy(space, "page", title)
	if !ok {
		return "", "", nil
	}

	entry, found, err := tracker.Lookup(space, file)
	if err != nil {
		return "", "", err
	}
	if !found {
		source, _, err := readSource(file)
		if err != nil {
			return "", "", nil //nolint:nilerr // indexed as readable; its own turn reports this
		}

		_, entry, found, err = tracker.ResolveRenamed(space, sha1Hash(string(source)))
		if err != nil || !found {
			return "", "", err
		}
	}
	if entry.PageID == "" {
		return "", "", nil
	}

	return entry.PageID, file, nil
}

// resolveRenamedFile returns the page a document published to under a previous
// path, for a document whose own path is not recorded.
//
// A rename is not visible as an event. It shows up as one path that has stopped
// appearing and another that has started, and the only thing tying them
// together is what the file contains -- which is how git recovers renames too,
// after the fact and by similarity rather than by being told.
//
// Returning (nil, nil) means no confident answer, and a new page is created.
// That is the right way to be wrong here: a duplicate is a nuisance, where
// rebinding onto the wrong page overwrites a document nobody asked to change.
func resolveRenamedFile(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
	sourceHash string,
) (*confluence.PageInfo, error) {
	if tracker == nil || meta == nil {
		return nil, nil
	}

	previous, entry, ok, err := tracker.ResolveRenamed(meta.Space, sourceHash)
	if err != nil {
		return nil, fmt.Errorf("unable to check whether %q was renamed: %w", file, err)
	}
	if !ok {
		return nil, nil
	}

	pg, err := api.GetPageByID(entry.PageID)
	if err != nil {
		if !errors.Is(err, confluence.ErrNotFound) {
			return nil, fmt.Errorf(
				"unable to load page %s recorded for %q: %w", entry.PageID, previous, err,
			)
		}
		// Recorded but gone, so there is nothing to rename. Drop the entry so it
		// stops being offered, and let the document be published afresh.
		log.Warn().Msgf("%s was published to page %s, which no longer exists", previous, entry.PageID)
		return nil, tracker.Forget(meta.Space, previous)
	}

	log.Info().Msgf(
		"%s has the content %s published as page %s; treating it as a rename rather than a new page",
		file, previous, pg.ID,
	)

	// The old path is gone. Left in place it would be reported as a deleted
	// file on every run from here on.
	if err := tracker.Forget(meta.Space, previous); err != nil {
		return nil, err
	}

	return pg, nil
}

// recordDirectoryPages remembers the pages standing for the directories a
// document sits under.
//
// mark creates them and nothing has been keeping track of them, so the last
// document under a directory going away used to leave its page behind with
// nobody aware it was ever mark's doing. Recorded, it turns up as a page with
// no source file like any other, and --on-orphan can be told what to do about
// it.
//
// The ids come from the published page's own ancestors rather than from the
// code that creates them: a document's ancestors end with exactly the pages its
// path asked for, and reading them costs nothing extra.
func recordDirectoryPages(
	tracker *manifest.Store,
	target *confluence.PageInfo,
	hierarchy *page.Hierarchy,
	file, space string,
) error {
	directories := hierarchy.Directories(file)
	if len(directories) == 0 || len(target.Ancestors) < len(directories) {
		return nil
	}

	// One ancestor per directory, at the end of the chain: whatever --parents
	// put above them comes first.
	tail := target.Ancestors[len(target.Ancestors)-len(directories):]

	for i, key := range directories {
		// A directory holding a document of its own has that document's entry
		// for its page already, and two entries claiming one page is the thing
		// the manifest complains about.
		if hierarchy.HasIndex(key) {
			continue
		}

		if err := tracker.Record(
			space, key, tail[i].ID, tail[i].Title, directoryHash(key),
		); err != nil {
			return fmt.Errorf("unable to record directory page for %q: %w", key, err)
		}
	}

	return nil
}
