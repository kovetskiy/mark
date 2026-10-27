// Finding and handling pages --on-orphan acts on.

package mark

import (
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
	tracker *manifest.Store,
	api *confluence.API,
	config Config,
	action string,
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

		scopeID, err := page.ResolveScope(api, space, config.OrphanUnder)
		if err != nil {
			return err
		}

		candidates := make([]page.Orphan, 0, len(orphans))
		for _, orphan := range orphans {
			candidate := page.Orphan{
				Path:   orphan.Path,
				PageID: orphan.Entry.PageID,
				Title:  orphan.Entry.Title,
			}

			inScope, err := page.InScope(api, scopeID, candidate)
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
		if action != page.OnOrphanReport {
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

				continue
			}

			handled, err = page.HandleOrphans(api, action, candidates, config.readOnly())
			if err != nil {
				return err
			}
		}

		if config.readOnly() {
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

			if err := tracker.Forget(space, path); err != nil {
				return fmt.Errorf("unable to update page manifest for %q: %w", path, err)
			}
		}
	}

	return nil
}
