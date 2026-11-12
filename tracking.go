// Resolving and recording pages through the --track-pages record.

package mark

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// previewTrackedResolution says what a real run would have done with a document
// the title lookup could not place.
//
// A dry run that reports a new page for every rename and retitle the real run
// would have handled in place is worse than no dry run, because it is confidently
// wrong about the one thing somebody is checking.
func (r *publishRun) previewTrackedResolution(ctx context.Context, doc *document) (*confluence.PageInfo, error) {
	meta, file := doc.meta, doc.file
	if r.tracker == nil || meta == nil {
		return nil, nil
	}

	// Errors stop the preview as they stop a real run: reporting "would create"
	// over a lookup that failed would hide the duplicate a real run refuses to make.
	pg, err := resolveTrackedPage(ctx, r.tracker, r.api, doc)
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

	pg, err = r.resolveRenamed(ctx, doc)
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
	ctx context.Context,
	tracker *manifest.Store,
	api *confluence.API,
	doc *document,
) (*confluence.PageInfo, error) {
	meta, file, key, warnings := doc.meta, doc.file, doc.key, doc.warnings
	if tracker == nil || meta == nil {
		return nil, nil
	}

	entry, ok, err := tracker.Lookup(ctx, meta.Space, key)
	if err != nil {
		return nil, fmt.Errorf("unable to read page mapping for %q: %w", key, err)
	}
	if !ok {
		return nil, nil
	}

	pg, inSpace, err := recordedPage(ctx, api, meta.Space, entry.PageID)
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
		warnings.Add(report.WarningMissingPage,
			"page %s, recorded for this document, no longer exists; a new page will be created",
			entry.PageID,
		)
		return nil, nil
	}
	if !inSpace {
		warnings.Add(report.WarningOtherSpace,
			"page %s, recorded for this document, is in %s, not space %q; publishing by title as if it had not been recorded",
			entry.PageID, pageSpace(pg), meta.Space,
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

// titleHeldError refuses a document a title that still names the page of
// another document of the run, one that publishes the page under a new title.
type titleHeldError struct {
	title string
	page  *confluence.PageInfo
	// holder is the document the page is recorded for, and retitled the
	// title it now declares.
	holder   input
	retitled string
}

func (e *titleHeldError) Error() string {
	return fmt.Sprintf(
		"the title %q belongs to page %s until %s, which publishes that page, has retitled it %q",
		e.title, e.page.ID, e.holder.file, e.retitled,
	)
}

// titleHolder returns, as the error refusing it, the other document of the
// run whose page the document's title finds while that document now declares
// another title; nil when there is none.
//
// The title lookup cannot tell that page from the document's own: it is the
// page of the title until its document has retitled it. Publishing to it
// would put two documents on one page and leave the title without one.
func (r *publishRun) titleHolder(
	ctx context.Context, file, key string, meta *metadata.Meta,
) (*titleHeldError, error) {
	if r.tracker == nil || meta == nil {
		return nil, nil
	}

	// Memoised, and the question resolving the page asks next.
	pg, err := r.api.FindPage(ctx, meta.Space, meta.Title, confluence.ContentType(meta.Type))
	if err != nil {
		return nil, fmt.Errorf("error while finding page %q: %w", meta.Title, err)
	}
	if pg == nil {
		return nil, nil
	}

	recorded, ok, err := r.tracker.RecordedFor(ctx, meta.Space, pg.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to read page mapping for page %s: %w", pg.ID, err)
	}
	if !ok || recorded == manifest.Key(key) {
		return nil, nil
	}

	holder, ok := r.titles.keyed(recorded)
	if !ok || holder.file == file {
		return nil, nil
	}

	// A holder that keeps the title, or publishes nothing, leaves the page
	// to be found by its title.
	declared := r.titles.readAhead(holder).meta
	if declared == nil || !declared.Publish() || declared.Title == "" ||
		declared.Space != meta.Space || declared.Type != meta.Type ||
		strings.EqualFold(declared.Title, pg.Title) {
		return nil, nil
	}

	return &titleHeldError{title: meta.Title, page: pg, holder: holder, retitled: declared.Title}, nil
}

// titleFreed reports, for a dry run, whether the page the document's title
// finds would have been retitled by its own document by the document's turn,
// leaving the title to a new page. It would when that document's new title
// is itself free, or freed the same way; visiting holds the documents asked
// about, so that titles swapped between documents are found as they are.
//
// A page --no-overwrite would leave alone keeps its title, and the error is
// the one a real run refuses the document with.
func (r *publishRun) titleFreed(
	ctx context.Context, file, key string, meta *metadata.Meta, visiting map[string]bool,
) (bool, error) {
	held, err := r.titleHolder(ctx, file, key, meta)
	if err != nil || held == nil || visiting[held.holder.file] {
		return false, err
	}

	declared := r.titles.readAhead(held.holder).meta

	if r.noOverwrite {
		drift, err := hasDrifted(ctx, r.api, r.tracker, declared.Space, held.holder.key, held.page)
		if err != nil {
			return false, err
		}
		if drift != "" {
			return false, held
		}
	}

	taken, err := r.api.FindPage(ctx, declared.Space, declared.Title, confluence.ContentType(declared.Type))
	if err != nil {
		return false, fmt.Errorf("error while finding page %q: %w", declared.Title, err)
	}
	if taken == nil {
		return true, nil
	}

	visiting[file] = true

	freed, err := r.titleFreed(ctx, held.holder.file, held.holder.key, declared, visiting)
	if refused := (*titleHeldError)(nil); errors.As(err, &refused) {
		// The holder is refused its own title, so it keeps this one.
		return false, held
	}

	return freed, err
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
func refreshStaleParents(
	ctx context.Context, tracker *manifest.Store, api *confluence.API, meta *metadata.Meta, titles *runTitles,
	warnings *report.Warnings,
) error {
	if tracker == nil {
		return nil
	}

	// Keys name the chain as the document declares it, which is not what
	// meta.Parents holds once an earlier entry has been rewritten. A chain
	// whose first parent was also renamed would otherwise be looked up under a
	// name it was never recorded under.
	declared := slices.Clone(meta.Parents)

	// The titles found nowhere, so a chain naming one twice is told of it once.
	unfollowed := map[string]bool{}

	for i, title := range declared {
		// FindPage is memoised, and ancestry resolution is about to ask the
		// same question, so this costs nothing on the common path where the
		// parent is exactly where it says it is.
		existing, err := api.FindPage(ctx, meta.Space, title, confluence.ContentPage)
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
		pageID, publisher, err := trackedPageOfDeclaredTitle(ctx, tracker, titles, meta.Space, title)
		if err != nil {
			return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
		}
		ok := pageID != ""
		if !ok {
			pageID, ok, err = tracker.LookupParent(ctx, meta.Space, page.ParentPathKey(declared[:i+1]))
			if err != nil {
				return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
			}
		}
		if !ok {
			pageID, ok, err = tracker.ResolveStaleTitle(ctx, meta.Space, title)
			if err != nil {
				return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
			}
			if !ok {
				if key := runTitleKey(meta.Space, "page", title); !unfollowed[key] {
					unfollowed[key] = true
					titles.warnAmbiguous(meta.Space, title, warnings)
				}

				continue
			}
		}

		renamed, inSpace, err := recordedPage(ctx, api, meta.Space, pageID)
		if err != nil {
			// Only a recorded page that is genuinely gone may be passed over.
			// Anything else is a failure to read, and carrying on with the old
			// title turns a network blip into a duplicate parent.
			if !errors.Is(err, confluence.ErrNotFound) {
				return fmt.Errorf(
					"unable to load page %s recorded for parent %q: %w", pageID, title, err,
				)
			}

			warnings.Add(report.WarningMissingPage,
				"parent %q was recorded as page %s, which no longer exists; leaving the title as written",
				title, pageID,
			)

			continue
		}
		if !inSpace {
			warnings.Add(report.WarningOtherSpace,
				"parent %q was recorded as page %s, which is in %s, not space %q; leaving the title as written",
				title, pageID, pageSpace(renamed), meta.Space,
			)

			continue
		}
		if renamed.Title == title {
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

// recordedPage reads a page an entry of space's manifest names, and whether it
// is in that space. A manifest is kept per space and speaks for that space's
// pages alone: one of its entries naming a page elsewhere -- moved there by
// hand, or written into the manifest from another -- is not its page, and
// following it would update, move or remove a page outside the space the run
// publishes to. A page whose space cannot be told is an error rather than one
// elsewhere: passed over, a retitled page would be published a second time.
func recordedPage(ctx context.Context, api *confluence.API, space, pageID string) (*confluence.PageInfo, bool, error) {
	pg, err := api.GetPageByIDExpanded(ctx, pageID, "ancestors,version,space")
	if err != nil {
		return nil, false, err
	}

	inSpace, known, err := pageInSpace(ctx, api, pg, space)
	if err != nil {
		return nil, false, err
	}
	if !known {
		return nil, false, fmt.Errorf("unable to tell which space page %s is in", pageID)
	}

	return pg, inSpace, nil
}

// pageInSpace reports whether a page read with its space expanded is in space,
// and whether that could be told at all. v1 names the space by key; v2 by id,
// with the key only when the space itself could be read, so the id decides
// when the key is missing.
func pageInSpace(
	ctx context.Context, api *confluence.API, pg *confluence.PageInfo, space string,
) (inSpace, known bool, err error) {
	switch {
	case pg.Space.Key != "":
		return strings.EqualFold(pg.Space.Key, space), true, nil
	case pg.SpaceID != "":
		id, err := api.GetSpaceID(ctx, space)
		if errors.Is(err, confluence.ErrNotFound) {
			// Not the page being gone, which callers take ErrNotFound for.
			return false, false, nil
		}
		if err != nil {
			return false, false, fmt.Errorf("unable to read space %s: %w", space, err)
		}

		return pg.SpaceID == id, true, nil
	default:
		return false, false, nil
	}
}

// pageSpace names the space a page is in, for a message.
func pageSpace(pg *confluence.PageInfo) string {
	switch {
	case pg.Space.Key != "":
		return fmt.Sprintf("space %q", pg.Space.Key)
	case pg.SpaceID != "":
		return "space " + pg.SpaceID
	default:
		return "a space mark could not read"
	}
}

// trackedPageOfDeclaredTitle returns the page recorded for the one document of
// the run that publishes the title as a page, and that document, if it has been
// published before. A document renamed since is found by its content, as
// resolveRenamedFile finds it when its own turn comes.
func trackedPageOfDeclaredTitle(
	ctx context.Context, tracker *manifest.Store, titles *runTitles, space, title string,
) (string, string, error) {
	in, ok := titles.declaredBy(space, "page", title)
	if !ok {
		return "", "", nil
	}

	entry, found, err := tracker.Lookup(ctx, space, in.key)
	if err != nil {
		return "", "", err
	}
	if !found {
		// Indexed, so read ahead already.
		_, entry, found, err = tracker.ResolveRenamedFor(ctx, space, in.key, titles.readAhead(in).sourceHash)
		if err != nil || !found {
			return "", "", err
		}
	}
	if entry.PageID == "" {
		return "", "", nil
	}

	return entry.PageID, in.file, nil
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
	ctx context.Context,
	tracker *manifest.Store,
	api *confluence.API,
	doc *document,
) (*confluence.PageInfo, error) {
	meta, file, warnings := doc.meta, doc.file, doc.warnings
	if tracker == nil || meta == nil {
		return nil, nil
	}

	previous, entry, ok, err := tracker.ResolveRenamedFor(ctx, meta.Space, doc.key, doc.sourceHash)
	if err != nil {
		return nil, fmt.Errorf("unable to check whether %q was renamed: %w", file, err)
	}
	if !ok {
		return nil, nil
	}

	pg, inSpace, err := recordedPage(ctx, api, meta.Space, entry.PageID)
	if err != nil {
		if !errors.Is(err, confluence.ErrNotFound) {
			return nil, fmt.Errorf(
				"unable to load page %s recorded for %q: %w", entry.PageID, previous, err,
			)
		}
		// Recorded but gone, so there is nothing to rename. Drop the entry so it
		// stops being offered, and let the document be published afresh.
		warnings.Add(report.WarningMissingPage,
			"%s, which this document was renamed from, was published to page %s, which no longer exists",
			previous, entry.PageID)
		return nil, tracker.Forget(ctx, meta.Space, previous)
	}
	if !inSpace {
		// Not followed, and dropped like an entry whose page is gone: the
		// document is published afresh and recorded under its new path with
		// the same content, and two entries sharing it would stop the next
		// rename being followed.
		warnings.Add(report.WarningOtherSpace,
			"%s, which this document was renamed from, was published to page %s, which is in %s, not space %q; publishing by title instead",
			previous, entry.PageID, pageSpace(pg), meta.Space)
		return nil, tracker.Forget(ctx, meta.Space, previous)
	}

	log.Info().Msgf(
		"%s has the content %s published as page %s; treating it as a rename rather than a new page",
		file, previous, pg.ID,
	)

	// The old entry is kept until the new path is recorded: it holds the
	// version --no-overwrite compares against, read through LookupMoved, and
	// Record carries that version across and drops the old path.
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
// code that creates them: they hold the pages its path asked for, and reading
// them costs nothing extra. Each directory is found among them by the parent
// title the document resolved it to, not by position: the ancestry check
// accepts a page sitting deeper than its path says, under a page somebody put
// there by hand, and a title names one page in a space.
//
// A directory only documents lie in is recorded under its key in
// directoryKeys, in their scope; any other by its path.
//
// Only a page mark created is recorded: it writes nothing to a directory's
// page, so one that was there before it is somebody else's, and --on-orphan
// must never be offered it.
func recordDirectoryPages(
	ctx context.Context,
	tracker *manifest.Store,
	created map[string]bool,
	target *confluence.PageInfo,
	hierarchy *page.Hierarchy,
	directoryKeys map[string]string,
	file, space string,
	parents []string,
) error {
	directories := hierarchy.Directories(file)
	if len(directories) == 0 || len(parents) < len(directories) {
		return nil
	}

	// One parent per directory, at the end of the chain: whatever --parents
	// put above them comes first.
	titles := parents[len(parents)-len(directories):]

	for i, key := range directories {
		// A directory holding a document of its own has that document's entry
		// for its page already, and two entries claiming one page is the thing
		// the manifest complains about.
		if hierarchy.HasIndex(key) {
			continue
		}

		// In any order: the ancestry check asks only that each parent is
		// somewhere above the page, so neither may this.
		for _, directory := range target.Ancestors {
			if !strings.EqualFold(directory.Title, titles[i]) {
				continue
			}

			recorded := key
			if documents, ok := directoryKeys[key]; ok {
				recorded = documents
			}

			own, err := ownDirectoryPage(ctx, tracker, created, space, recorded, directory.ID)
			if err != nil {
				return fmt.Errorf("unable to look up directory page for %q: %w", key, err)
			}
			if !own {
				break
			}

			if err := tracker.Record(
				ctx,
				space, recorded, directory.ID, directory.Title, page.DirectoryHash(recorded),
			); err != nil {
				return fmt.Errorf("unable to record directory page for %q: %w", key, err)
			}

			break
		}
	}

	return nil
}

// ownDirectoryPage reports whether the page found for a directory is one mark
// created: in this run, or in an earlier one that recorded it, under this key
// or under one the run finds gone, as after the directory was renamed.
//
// An entry naming another page under the key is dropped when the page found
// is not mark's: the directory is there, so that page is no orphan, and it no
// longer stands for the directory.
func ownDirectoryPage(
	ctx context.Context,
	tracker *manifest.Store,
	created map[string]bool,
	space, key, pageID string,
) (bool, error) {
	entry, recorded, err := tracker.Recorded(ctx, space, key)
	if err != nil {
		return false, err
	}
	if created[pageID] || recorded && entry.PageID == pageID {
		return true, nil
	}

	_, moved, err := tracker.LookupMoved(ctx, space, key, pageID)
	if err != nil || moved || !recorded {
		return moved, err
	}

	return false, tracker.Forget(ctx, space, key)
}

// recordPublished records in the manifest the page a document was published
// to, and the version --no-overwrite compares against on the next run.
//
// Recorded after the write, not before it. The mapping asserts what is on the
// page -- the title it carries and a fingerprint of the source that produced
// its body -- and the manifest is saved however the run ends, so a body that
// was refused as malformed, or an update that failed, must not leave the
// mapping claiming both. The fingerprint is what rename detection matches on.
func (r *publishRun) recordPublished(ctx context.Context, doc *document, target *confluence.PageInfo) error {
	meta, file := doc.meta, doc.file
	if r.tracker == nil || meta == nil {
		return nil
	}

	if err := r.tracker.Record(ctx, meta.Space, doc.key, target.ID, meta.Title, doc.sourceHash); err != nil {
		return fmt.Errorf("unable to record page mapping for %q: %w", doc.key, err)
	}

	if r.hierarchy != nil && !meta.DeclaredParents {
		if err := recordDirectoryPages(ctx, r.tracker, r.createdParents, target, r.hierarchy, r.directoryKeys, file, meta.Space, meta.Parents); err != nil {
			return err
		}
	}

	// UpdatePage advances the version it was given, so this reads the same
	// either way: a run that wrote nothing still records what it found, so
	// that a page nobody touches gets a version on the first run rather than
	// staying unguarded forever.
	if err := r.tracker.RecordVersion(ctx, meta.Space, doc.key, target.Version.Number); err != nil {
		return fmt.Errorf("unable to record page version for %q: %w", doc.key, err)
	}

	return nil
}
