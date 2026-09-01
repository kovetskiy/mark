package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// What to do about a page whose source file is gone.
const (
	// OnOrphanReport says so and does nothing, which is what mark has always
	// done and remains the default.
	OnOrphanReport = "report"

	// OnOrphanArchive archives the page. Cloud only.
	OnOrphanArchive = "archive"

	// OnOrphanDelete moves the page to the trash, where a person can still
	// change their mind.
	OnOrphanDelete = "delete"
)

// ParseOnOrphan reads the value given to --on-orphan.
func ParseOnOrphan(value string) (string, error) {
	switch action := strings.ToLower(strings.TrimSpace(value)); action {
	case "", OnOrphanReport:
		return OnOrphanReport, nil
	case OnOrphanArchive:
		return OnOrphanArchive, nil
	case OnOrphanDelete:
		return OnOrphanDelete, nil
	default:
		return "", fmt.Errorf(
			"unknown --on-orphan value %q: expected %s, %s or %s",
			value, OnOrphanReport, OnOrphanArchive, OnOrphanDelete,
		)
	}
}

// Orphan is a tracked page whose source file was not seen in the run.
type Orphan struct {
	Path   string
	PageID string
	Title  string
}

// OrphanScope limits which orphans may be acted on.
type OrphanScope struct {
	// Under is a page or folder whose descendants alone are in scope. Empty
	// means the whole space, as far as the run's own file pattern reaches.
	Under string
}

// ResolveScope finds the page or folder --orphan-under names.
//
// A title or an id, and a page or a folder, because the thing people want to
// put a boundary around is a place in the tree and mark's tree has both in it.
func ResolveScope(api *confluence.API, spaceKey, under string) (string, error) {
	under = strings.TrimSpace(under)
	if under == "" {
		return "", nil
	}

	// An id, if it looks like one and something is there.
	if isNumeric(under) {
		if found, err := api.GetPageByID(under); err == nil && found != nil {
			return found.ID, nil
		}
	}

	found, err := api.FindPage(spaceKey, under, "page")
	if err != nil {
		return "", fmt.Errorf("unable to find %q in space %q: %w", under, spaceKey, err)
	}
	if found != nil {
		return found.ID, nil
	}

	folder, err := api.FindFolder(spaceKey, under, "")
	if err != nil {
		return "", fmt.Errorf("unable to find folder %q in space %q: %w", under, spaceKey, err)
	}
	if folder != nil {
		return folder.ID, nil
	}

	return "", fmt.Errorf(
		"--orphan-under names %q, which is not a page or folder in space %q",
		under, spaceKey,
	)
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return s != ""
}

// InScope reports whether an orphan sits beneath the scope's boundary.
//
// Answered from the page's own ancestors rather than by listing the boundary's
// descendants: there are few orphans and there may be very many descendants.
func InScope(api *confluence.API, scopeID string, orphan Orphan) (bool, error) {
	if scopeID == "" {
		return true, nil
	}

	page, err := api.GetPageByIDExpanded(orphan.PageID, "ancestors")
	if err != nil {
		return false, fmt.Errorf("unable to read page %s: %w", orphan.PageID, err)
	}

	if page == nil {
		// Already gone from Confluence. Out of scope for acting on, and the
		// caller forgets it either way.
		return false, nil
	}

	for _, ancestor := range page.Ancestors {
		if ancestor.ID == scopeID {
			return true, nil
		}
	}

	return false, nil
}

// HandleOrphans acts on the pages whose source files are gone.
//
// Every orphan given here is acted on: the caller decides what is in scope,
// because it has to name the same set in what it reports and in what it stops
// tracking. Asking again here meant a second GetPageByIDExpanded for every
// candidate -- twice the API cost of the one operation in mark that deletes
// pages -- for an answer that had just been computed.
//
// Returns the paths that were dealt with, which the caller stops tracking. A
// page that could not be acted on stays in the manifest, so the next run finds
// it again rather than losing sight of it.
func HandleOrphans(
	api *confluence.API,
	action string,
	orphans []Orphan,
	dryRun bool,
) ([]string, error) {
	var handled []string

	for _, orphan := range orphans {
		if action == OnOrphanReport {
			handled = append(handled, orphan.Path)

			continue
		}

		// Trashing a page takes its children with it, and those may be pages
		// nobody in this repository ever wrote.
		children, err := api.GetChildPages(orphan.PageID)
		if err != nil {
			return handled, fmt.Errorf("unable to list children of %s: %w", orphan.PageID, err)
		}

		if len(children) > 0 {
			log.Warn().Msgf(
				"page %q has %d child page(s) and is left alone: removing it would take them too",
				orphan.Title, len(children),
			)

			continue
		}

		if dryRun {
			log.Info().Msgf("page %q would be %sd", orphan.Title, action)
			handled = append(handled, orphan.Path)

			continue
		}

		log.Info().Msgf("%s page %q (%s)", action, orphan.Title, orphan.PageID)

		if action == OnOrphanArchive {
			err = api.ArchivePage(orphan.PageID)
		} else {
			err = api.DeletePage(orphan.PageID)
		}
		if err != nil {
			return handled, fmt.Errorf("unable to %s page %q: %w", action, orphan.Title, err)
		}

		handled = append(handled, orphan.Path)
	}

	return handled, nil
}
