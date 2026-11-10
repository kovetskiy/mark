// Finding and handling pages --on-orphan acts on.

package mark

import (
	"context"
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// handleOrphans deals with the pages whose source files were not seen.
//
// Reporting, acting and forgetting are one thing because they have to agree
// about which pages are being talked about. When they were separate, a scope
// narrowed what was archived or deleted while leaving reporting to name
// everything and forgetting to drop everything -- so a page deliberately put
// out of scope was announced and then lost track of anyway.
//
// A page outside the scope is not mark's business on this run: not reported,
// not touched, and still remembered, so that a later run with a wider scope
// still knows about it.
func handleOrphans(
	ctx context.Context,
	tracker *manifest.Store,
	api *confluence.API,
	config Config,
	action report.OnOrphan,
	hadErrors bool,
	results *report.Report,
) error {
	for _, space := range tracker.Spaces() {
		orphans := tracker.OrphanEntries(space)
		if len(orphans) == 0 {
			continue
		}

		if hadErrors {
			// Some files did not process, which is indistinguishable from those
			// files being gone. Saying anything now would be actively
			// misleading, and acting on it worse.
			log.Debug().Msgf(
				"space %q has %d unpublished tracked pages, not reported because this run had errors",
				space, len(orphans),
			)

			continue
		}

		scopeID, err := page.ResolveScope(ctx, api, space, config.Tracking.OrphanUnder)
		if err != nil {
			return err
		}

		candidates := make([]page.Orphan, 0, len(orphans))
		globs := make(map[string]string, len(orphans))
		for _, orphan := range orphans {
			globs[orphan.Path] = orphan.Entry.Glob
			candidate := page.Orphan{
				Path:   orphan.Path,
				PageID: orphan.Entry.PageID,
				Title:  orphan.Entry.Title,
			}

			inScope, err := page.InScope(ctx, api, scopeID, candidate)
			if err != nil {
				return err
			}

			if inScope {
				candidates = append(candidates, candidate)
			}
		}

		if len(candidates) == 0 {
			continue
		}

		paths := make([]string, 0, len(candidates))
		byPath := make(map[string]page.Orphan, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.Path)
			byPath[candidate.Path] = candidate
		}

		log.Info().Msgf(
			"space %q: %d tracked page(s) had no matching source file in this run: %s",
			space, len(paths), strings.Join(paths, ", "),
		)

		handled := paths
		if action != report.OnOrphanReport {
			// A run that published nothing in a space says nothing about that
			// space: a failed checkout and every document having been deleted
			// look exactly alike.
			//
			// A second line of defence rather than the first -- a pattern
			// matching no files stops the run well before this -- and kept
			// because the cost of it being needed once is a space emptied by a
			// bad checkout.
			if tracker.Published(space) == 0 {
				log.Warn().Msgf(
					"space %q: nothing was published in this run, so its %d orphaned page(s) are left alone",
					space, len(candidates),
				)

				if !config.Mode.readOnly() {
					for _, candidate := range candidates {
						reportKept(results, candidate, fmt.Sprintf(
							"nothing was published in space %q in this run", space,
						))
					}
				}

				continue
			}

			removable := candidates
			if config.Tracking.Partial {
				removable = notOwnPattern(tracker.RunPattern(), candidates, globs, config.Mode.readOnly(), results)
			}

			var kept map[string]string
			handled, kept, err = page.HandleOrphansKeeping(ctx, api, action, removable, config.Mode.readOnly())
			if err != nil {
				return err
			}

			if !config.Mode.readOnly() {
				for _, candidate := range removable {
					if reason, ok := kept[candidate.Path]; ok {
						reportKept(results, candidate, reason)
					}
				}
			}
		}

		if config.Mode.readOnly() {
			continue
		}

		// Only what was actually dealt with is forgotten. A page left alone --
		// holding children, or out of scope -- stays in the manifest so a later
		// run finds it again instead of losing sight of it.
		for _, path := range handled {
			// Reported whatever the action, "report" included: that is the
			// one action whose whole point is to be heard, and it used to be
			// the one left out. The page is named from the manifest, since
			// the file that would have named it is the thing that is gone.
			candidate := byPath[path]
			results.AddOrphan(report.Orphan{
				File: path, PageID: candidate.PageID, Title: candidate.Title, Action: action,
			})

			// A partial run that only reports leaves the entry for the run
			// owning the file, whose --on-orphan may do more than report.
			if config.Tracking.Partial && action == report.OnOrphanReport {
				continue
			}

			if err := tracker.Forget(ctx, space, path); err != nil {
				return fmt.Errorf("unable to update page manifest for %q: %w", path, err)
			}
		}
	}

	return nil
}

// notOwnPattern is the orphans a partial run may archive or delete: those
// recorded under a pattern other than its own. A partial run given the very
// pattern that recorded a file is not narrower than the run owning it, and on
// a checkout holding only some files would read every other one as deleted.
// The rest are left where they are, and stay tracked for the owning run.
func notOwnPattern(
	pattern string, candidates []page.Orphan, globs map[string]string,
	readOnly bool, results *report.Report,
) []page.Orphan {
	removable := make([]page.Orphan, 0, len(candidates))
	for _, candidate := range candidates {
		if globs[candidate.Path] != pattern {
			removable = append(removable, candidate)
			continue
		}

		log.Warn().Msgf(
			"%s: not removing page %s, since --partial was given the pattern that recorded it, %q; "+
				"name the paths a change touched instead",
			candidate.Path, candidate.PageID, pattern,
		)
		if !readOnly {
			reportKept(results, candidate, "--partial was given the pattern that recorded it, not the paths a change touched")
		}
	}

	return removable
}

// reportKept records an orphan --on-orphan asked to remove that was left where
// it is, and why. It stays in the manifest, so a later run finds it again.
func reportKept(results *report.Report, candidate page.Orphan, reason string) {
	results.AddOrphan(report.Orphan{
		File: candidate.Path, PageID: candidate.PageID, Title: candidate.Title,
		Action: report.OnOrphanReport, Reason: reason,
	})
}
