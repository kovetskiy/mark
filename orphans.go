// Finding and handling pages --on-orphan acts on.

package mark

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/internal/globpattern"
	"github.com/kovetskiy/mark/v17/internal/logctx"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
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
	// Spelled as a document given it publishes to it: metadata trims the
	// spaces around a space key.
	runSpace := strings.Trim(config.Page.Space, " ")

	for _, space := range tracker.Spaces() {
		// The space a run is given is the one it speaks for. Another is in the
		// run only because a document's Space header names it, which says
		// where that document goes and nothing of the other pages tracked
		// there: they are left, still tracked, for a run given that space.
		own := runSpace == "" || strings.EqualFold(runSpace, space)

		orphans := tracker.OrphanEntries(space)
		if len(orphans) == 0 {
			continue
		}

		if hadErrors {
			// Some files did not process, which is indistinguishable from those
			// files being gone. Saying anything now would be actively
			// misleading, and acting on it worse.
			logctx.From(ctx).Debug().Msgf(
				"space %q has %d unpublished tracked pages, not reported because this run had errors",
				space, len(orphans),
			)

			continue
		}

		if !own {
			inScope, err := otherSpaceInScope(ctx, api, space, config.Tracking.OrphanUnder, orphans)
			if err != nil {
				return err
			}
			if len(inScope) > 0 {
				leaveOtherSpace(ctx, config, space, runSpace, inScope, results)
			}

			continue
		}

		scopeID, err := page.ResolveScope(ctx, api, space, config.Tracking.OrphanUnder)
		if err != nil {
			return err
		}

		candidates := make([]page.Orphan, 0, len(orphans))
		// A partial run that only reports leaves an entry for the run whose
		// pattern recorded it, whose --on-orphan may do more than report. One
		// recorded under its own path alone has no such run: kept, it would be
		// reported by every partial run naming it and dealt with by none.
		keep := make(map[string]bool, len(orphans))
		globOf := make(map[string]string, len(orphans))
		// The candidates in another space, with why they are left alone. Their
		// space is read only when something may be removed from this one.
		elsewhere := map[string]string{}
		removing := action != report.OnOrphanReport && tracker.Published(space) > 0
		for _, orphan := range orphans {
			keep[orphan.Path] = config.Tracking.Partial && action == report.OnOrphanReport &&
				orphan.Entry.Glob != globpattern.Escape(orphan.Path)
			candidate := page.Orphan{
				Path:   orphan.Path,
				PageID: orphan.Entry.PageID,
				Title:  orphan.Entry.Title,
				Space:  space,
			}

			var inScope bool
			if !removing {
				inScope, err = page.InScope(ctx, api, scopeID, candidate)
			} else {
				var located *confluence.PageInfo
				located, err = locateOrphan(ctx, api, candidate.PageID, scopeID != "")
				if err != nil {
					return err
				}
				if located != nil {
					var inSpace, known bool
					inSpace, known, err = pageInSpace(ctx, api, located, space)
					if err != nil {
						return err
					}
					switch {
					case !known:
						// Left alone too: removing it could reach outside the space.
						elsewhere[candidate.Path] = fmt.Sprintf(
							"mark could not tell which space it is in, and space %q's page manifest records it", space,
						)
					case !inSpace:
						elsewhere[candidate.Path] = fmt.Sprintf(
							"it is in %s, not space %q, whose page manifest records it", pageSpace(located), space,
						)
					}
				}

				inScope, err = page.InScopeOf(ctx, api, scopeID, located)
			}
			if err != nil {
				return err
			}

			if inScope {
				candidates = append(candidates, candidate)
				globOf[orphan.Path] = orphan.Entry.Glob
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

		logctx.From(ctx).Info().Msgf(
			"space %q: %d tracked page(s) had no matching source file in this run: %s",
			space, len(paths), strings.Join(paths, ", "),
		)
		warnGoneKeys(ctx, config, space, paths, globOf)

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
				logctx.From(ctx).Warn().Msgf(
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

			// The same, for the run's files and each document scope on its
			// own: pages one of them published elsewhere in the space say
			// nothing about the other's. A partial run's document scope is
			// held to more, since the caller's key list decides what is gone:
			// some key of it tracked in the space has to still be there.
			acting := make([]page.Orphan, 0, len(candidates))
			survivors := map[string]int{}
			holding := tracker.HoldingPage(space)
			for _, candidate := range candidates {
				glob := globOf[candidate.Path]
				left, counted := survivors[glob]
				if !counted {
					left = tracker.Survivors(space, glob)
					survivors[glob] = left
				}

				if left == 0 {
					var reason string
					if listedScope(config, glob) {
						reason = fmt.Sprintf(
							"no key of %s tracked in space %q before this run is published or named in DocumentKeys, "+
								"which a failed render or a bad key list looks exactly like", glob, space,
						)
					} else {
						reason = fmt.Sprintf("nothing under %s was published in space %q in this run", glob, space)
					}
					logctx.From(ctx).Warn().Msgf("space %q: %s, so %s's page is left alone", space, reason, candidate.Path)

					if !config.Mode.readOnly() {
						reportKept(results, candidate, reason)
					}

					continue
				}

				// A manifest speaks for its own space's pages alone, however one
				// of its entries came to name a page elsewhere. The entry is
				// kept, so that each run reports the page again until somebody
				// corrects it.
				reason, alone := elsewhere[candidate.Path]

				// Removing the page the manifest is kept on would take the
				// manifest with it, and every later tracked run would stop there.
				if !alone && holding != "" && candidate.PageID == holding {
					reason, alone = fmt.Sprintf("the page manifest of space %q is kept on it", space), true
				}

				if alone {
					logctx.From(ctx).Warn().Msgf("page %q (%s) is left alone: %s", candidate.Title, candidate.PageID, reason)

					if !config.Mode.readOnly() {
						reportKept(results, candidate, reason)
					}

					continue
				}

				acting = append(acting, candidate)
			}

			var kept map[string]string
			handled, kept, err = page.HandleOrphansKeeping(ctx, api, action, acting, config.Mode.readOnly())
			if err != nil {
				return err
			}

			if !config.Mode.readOnly() {
				for _, candidate := range candidates {
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

			if keep[path] {
				continue
			}

			if err := tracker.Forget(ctx, space, path); err != nil {
				return fmt.Errorf("unable to update page manifest for %q: %w", path, err)
			}
		}
	}

	return nil
}

// leaveOtherSpace says which tracked pages of a space other than the run's own
// the run published nothing to, and lists them as left where they are,
// whatever --on-orphan was to do with them. Such a page is that of a file that
// is gone or of a document whose Space header now names another space, so the
// wording claims neither.
func leaveOtherSpace(ctx context.Context, config Config, space, runSpace string, orphans []manifest.Orphan, results *report.Report) {
	reason := fmt.Sprintf(
		"space %q is not the run's space, %q; it is left for a run whose --space is %s, or to be removed by hand",
		space, runSpace, space,
	)

	paths := make([]string, 0, len(orphans))
	for _, orphan := range orphans {
		paths = append(paths, orphan.Path)
		if !config.Mode.readOnly() {
			reportKept(results, page.Orphan{
				Path: orphan.Path, PageID: orphan.Entry.PageID, Title: orphan.Entry.Title,
			}, reason)
		}
	}

	logctx.From(ctx).Warn().Msgf(
		"space %q: %d tracked page(s) were not published by this run and are left alone, still tracked, "+
			"for a run whose --space is %s or to be removed by hand, since space %q is not the run's space, %q: %s",
		space, len(paths), space, space, runSpace, strings.Join(paths, ", "),
	)
}

// otherSpaceInScope returns the orphans of a space the run was not given that
// lie beneath --orphan-under, as in the run's own space. A space with no page
// or folder of that name has none there, and the run, which removes nothing in
// that space, has no cause to fail over it.
func otherSpaceInScope(
	ctx context.Context, api *confluence.API, space, under string, orphans []manifest.Orphan,
) ([]manifest.Orphan, error) {
	scopeID, err := page.ResolveScope(ctx, api, space, under)
	if errors.Is(err, page.ErrScopeNotFound) {
		logctx.From(ctx).Debug().Msgf("space %q has no page or folder %q, so none of its tracked pages is in scope", space, under)

		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	inScope := make([]manifest.Orphan, 0, len(orphans))
	for _, orphan := range orphans {
		in, err := page.InScope(ctx, api, scopeID, page.Orphan{
			Path: orphan.Path, PageID: orphan.Entry.PageID, Title: orphan.Entry.Title, Space: space,
		})
		if err != nil {
			return nil, err
		}
		if in {
			inScope = append(inScope, orphan)
		}
	}

	return inScope, nil
}

// listedScope reports whether the orphans of glob are keys a partial run's
// DocumentKeys left out. A partial run finds a scope's orphans only when
// given DocumentKeys, so its document orphans need nothing more to tell.
func listedScope(config Config, glob string) bool {
	return config.Tracking.Partial && manifest.IsDocumentScope(glob)
}

// warnGoneKeys says, per document scope of a partial run, how many of its
// keys the run takes as gone: a key left out of DocumentKeys by mistake reads
// exactly as one removed, and --on-orphan acts on it alike.
func warnGoneKeys(ctx context.Context, config Config, space string, paths []string, globOf map[string]string) {
	gone := map[string][]string{}
	for _, path := range paths {
		if glob := globOf[path]; listedScope(config, glob) {
			gone[glob] = append(gone[glob], path)
		}
	}

	for _, glob := range slices.Sorted(maps.Keys(gone)) {
		logctx.From(ctx).Warn().Msgf(
			"space %q: %d key(s) of %s are neither published nor named in DocumentKeys, so they are taken as gone: %s",
			space, len(gone[glob]), glob, strings.Join(gone[glob], ", "),
		)
	}
}

// locateOrphan reads the page of an orphan --on-orphan is to remove, with its
// space, and with its ancestors when a scope is to be checked: one read for
// both questions. A page already gone is nil, for HandleOrphansKeeping to find
// gone.
func locateOrphan(ctx context.Context, api *confluence.API, pageID string, scoped bool) (*confluence.PageInfo, error) {
	expand := "space"
	if scoped {
		expand = "ancestors,space"
	}

	pg, err := api.GetPageByIDExpanded(ctx, pageID, expand)
	if errors.Is(err, confluence.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read page %s: %w", pageID, err)
	}

	return pg, nil
}

// reportKept records an orphan --on-orphan asked to remove that was left where
// it is, and why. It stays in the manifest, so a later run finds it again.
func reportKept(results *report.Report, candidate page.Orphan, reason string) {
	results.AddOrphan(report.Orphan{
		File: candidate.Path, PageID: candidate.PageID, Title: candidate.Title,
		Action: report.OnOrphanReport, Reason: reason,
	})
}
