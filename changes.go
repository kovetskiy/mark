// Change detection behind --changes-only and --no-overwrite.

package mark

import (
	"context"
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/fingerprint"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/rs/zerolog/log"
)

// previewChange says whether a real run would change the page, using the same
// comparison it does for the body, and logs the answer.
//
// others are what a real run would do to the page besides writing its body --
// moving it, say -- which it does whatever the body comparison finds, so any of
// them makes the page one that would change. They come back as the reason.
func previewChange(
	ctx context.Context,
	body string,
	existing *confluence.PageInfo,
	relinked bool,
	others []string,
	file string,
	config PublishConfig,
	meta *metadata.Meta,
	std *stdlib.Lib,
) (report.Status, string, error) {
	if existing == nil {
		log.Info().Msgf("%s: page would be created", file)

		return report.StatusWouldCreate, "", nil
	}

	var layout, sidebar, appearance, emoji, title string
	if meta != nil {
		layout, sidebar, appearance, emoji, title = meta.Layout, meta.Sidebar, string(meta.ContentAppearance), meta.Emoji, meta.Title
	}

	wrapped, err := wrapLayout(std, layout, sidebar, body)
	if err != nil {
		return "", "", err
	}

	hash := fingerprint.Content(wrapped, fingerprint.Properties{
		Appearance:        appearance,
		DefaultAppearance: defaultAppearance(config.Page.ContentAppearance),
		Emoji:             emoji,
	})
	retitled := meta != nil && existing.Title != title
	reason := strings.Join(others, "; ")

	switch {
	case retitled:
		log.Info().Msgf("%s: page %q would be retitled to %q", file, existing.Title, title)
	case relinked:
		log.Info().Msgf("%s: page %q would be updated, with an attachment it does not have yet", file, existing.Title)
	case fingerprint.FromVersionMessage(existing.Version.Message) == hash:
		if len(others) == 0 {
			log.Info().Msgf("%s: page %q is already up to date", file, existing.Title)

			return report.StatusUnchanged, "", nil
		}

		log.Info().Msgf("%s: page %q has an unchanged body but %s", file, existing.Title, reason)
	default:
		log.Info().Msgf("%s: page %q would be updated", file, existing.Title)
	}

	return report.StatusWouldUpdate, reason, nil
}

// hasDrifted reports whether a page has been changed by somebody other than
// mark since mark last published it, put the way the report says it: why
// --no-overwrite leaves the page alone, naming the version mark wrote, or ""
// when it has not drifted. Shared by a real run and the --changes-only
// preview, so the two cannot disagree about either.
//
// The comparison is against the version number rather than the page's content:
// Confluence rewrites storage markup on save often enough that comparing bodies
// would report a difference on pages nobody had touched.
//
// An entry with no recorded version cannot answer the question -- it was
// written before versions were tracked, or by a run without --no-overwrite --
// and a page is given the benefit of the doubt rather than frozen.
//
// A page this run has already moved on Server or Data Center is compared as it
// was before the move. The move is an update there, so it lands a version
// before this check is reached; that version is mark's, and taking it for an
// edit left every moved page unpublished and reported on every run after. An
// edit made before the move still shows, since the version the move started
// from is then not the one recorded either.
//
// A source file that moved is compared against the entry of the path it moved
// from, for the one run on which its own path has none.
func hasDrifted(
	ctx context.Context,
	api *confluence.API,
	tracker *manifest.Store,
	spaceKey, file string,
	target *confluence.PageInfo,
) (string, error) {
	entry, ok, err := tracker.Lookup(ctx, spaceKey, file)
	if err != nil {
		return "", fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
	}

	// A document moved, found by its unchanged title or by its content, has
	// no entry under its new path yet: the baseline is still in the one it moved
	// from, which Record only carries across after this. Asking the new path
	// alone read that as a page with no baseline, and the very run that moved
	// the file overwrote an edit --no-overwrite exists to keep.
	if !ok {
		entry, ok, err = tracker.LookupMoved(ctx, spaceKey, file, target.ID)
		if err != nil {
			return "", fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
		}
	}

	if !ok || entry.Version == 0 || entry.PageID != target.ID {
		// Without a manifest nothing says which version mark last wrote, so
		// an edit made in Confluence cannot be told from none. Overwriting
		// on that doubt could happen on every run for as long as the page
		// is missing, not once as on a first tracked run. A page this run
		// created, such as an empty parent, and still at the version of
		// mark's last write to it is mark's own.
		if tracker.Untracked(spaceKey) {
			if written, ok := api.CreatedVersion(target.ID); ok && written == target.Version.Number {
				return "", nil
			}
			return "not checked for edits made in Confluence: the space has no page manifest " +
				"(its manifest page is missing)", nil
		}
		return "", nil
	}

	current := target.Version.Number
	if before, moved := api.ReparentedFrom(target.ID, current); moved {
		current = before
	}

	if current == entry.Version {
		return "", nil
	}

	return fmt.Sprintf(
		"edited in Confluence since mark published it (version %d, mark wrote %d)",
		target.Version.Number, entry.Version,
	), nil
}

// leaveDrifted reports, records and returns true for a page --no-overwrite
// keeps: one edited in Confluence since mark last published it.
func (r *publishRun) leaveDrifted(ctx context.Context, doc *document, dest *destination) (bool, error) {
	meta, file, target := doc.meta, doc.file, dest.page
	if !r.noOverwrite || dest.created || r.tracker == nil || meta == nil || target == nil {
		return false, nil
	}

	drift, err := hasDrifted(ctx, r.api, r.tracker, meta.Space, file, target)
	if err != nil {
		return false, err
	}

	if drift == "" {
		return false, nil
	}

	log.Warn().Msgf("page %q was %s; leaving it alone", target.Title, drift)

	r.addPage(doc, report.Page{
		File: file, Status: report.StatusSkipped,
		Reason: drift,
		Space:  spaceOf(meta), Title: target.Title,
		PageID: target.ID, URL: r.api.BaseURL() + target.Links.Full,
	})

	// Recorded, so the page still counts as seen and is not mistaken
	// for an orphan and deleted. The version is deliberately not
	// updated: until somebody resolves the difference, every run should
	// say so again.
	if err := r.tracker.Record(ctx, meta.Space, file, target.ID, target.Title, doc.sourceHash); err != nil {
		return false, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
	}

	return true, nil
}

// changeCheck returns the version message to publish html under and, under
// --changes-only, whether the page needs writing at all.
func (r *publishRun) changeCheck(doc *document, dest *destination, html string) (string, bool) {
	if !r.config.Update.ChangesOnly {
		return r.config.Update.VersionMessage, true
	}

	target := dest.page
	contentAppearance, emoji := appearanceOf(doc.meta)

	contentHash := fingerprint.Content(html, fingerprint.Properties{
		Appearance:        contentAppearance,
		DefaultAppearance: defaultAppearance(r.config.Page.ContentAppearance),
		Emoji:             emoji,
	})
	log.Debug().Msgf("content hash: %s", contentHash)

	shouldUpdatePage := true
	if previous := fingerprint.FromVersionMessage(target.Version.Message); previous != "" {
		log.Debug().Msgf("previous content hash: %s", previous)
		if previous == contentHash {
			if dest.titleChanged {
				log.Info().Msgf("page %q is unchanged but is being retitled", target.Title)
			} else {
				log.Info().Msgf("page %q is already up to date", target.Title)
				shouldUpdatePage = false
			}
		}
	}

	return fingerprint.VersionMessage(r.config.Update.VersionMessage, contentHash), shouldUpdatePage
}

// previewSettingChanges is what a real run would change on a previewed page
// besides its body and attachments, for previewChange's others.
func (r *publishRun) previewSettingChanges(ctx context.Context, doc *document, found pagePreview) ([]string, error) {
	meta, previewed := doc.meta, found.page
	if previewed == nil || meta == nil {
		return nil, nil
	}

	var others []string

	if page.WouldMove(ctx, r.api, previewed, found.parent, meta.Parents, found.misplaced) {
		others = append(others, "it would be moved under the parent its headers declare")
	}

	add, del, err := r.api.LabelChanges(ctx, previewed, meta.Labels, r.config.Update.AppendLabels)
	if err != nil {
		return nil, err
	}
	if len(add) > 0 || len(del) > 0 {
		others = append(others, "its labels would change")
	}

	if doc.restrictions != nil {
		plan, err := r.api.PlanRestrictions(ctx, previewed, doc.restrictions, r.config.Update.EditLock, false)
		if err != nil {
			return nil, err
		}
		if plan.Changes() {
			others = append(others, "its restrictions would change")
		}
	}

	return others, nil
}

// defaultAppearance is the appearance a page with no Content-Appearance of its
// own takes. Config's checks have refused a value that does not parse.
func defaultAppearance(value metadata.Appearance) string {
	appearance, _ := metadata.ResolveContentAppearance(string(value))

	return string(appearance)
}
