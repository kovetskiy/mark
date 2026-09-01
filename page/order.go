package page

import (
	"fmt"
	"sort"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// Ordered is a page a document asked to be placed at a particular position
// among its siblings.
type Ordered struct {
	PageID   string
	ParentID string
	Title    string
	Order    int
}

// OrderChildren arranges pages under each parent into the order their documents
// asked for.
//
// Only pages named here are touched, and only relative to each other. A run
// covering part of a repository has no idea what else lives under those
// parents, and dragging strangers about because they happen to share a parent
// would be well beyond what was asked for.
//
// Nothing is moved that is already in the right relative order. That is not an
// optimisation but the point: ordering runs on every publish, and a version
// that reissued every move each time would fill page histories with churn and
// make the feature unusable in CI. A run that changes nothing performs no
// writes at all.
func OrderChildren(api *confluence.API, dryRun bool, pages []Ordered) error {
	byParent := map[string][]Ordered{}
	for _, p := range pages {
		if p.ParentID == "" {
			// Nothing to be ordered relative to. Confluence's before/after also
			// misbehave against top-level targets, so this is left alone -- but
			// said out loud, because a document that asked for a position and
			// silently got none looks exactly like one that never asked.
			log.Warn().Msgf(
				"page %q asked to be ordered %d but its parent is not known, so it is left where it is",
				p.Title, p.Order,
			)

			continue
		}
		byParent[p.ParentID] = append(byParent[p.ParentID], p)
	}

	parents := make([]string, 0, len(byParent))
	for parent := range byParent {
		parents = append(parents, parent)
	}
	sort.Strings(parents)

	for _, parent := range parents {
		if err := orderUnder(api, dryRun, parent, byParent[parent]); err != nil {
			return err
		}
	}

	return nil
}

func orderUnder(api *confluence.API, dryRun bool, parentID string, wanted []Ordered) error {
	if len(wanted) < 2 {
		// A single page has nothing to be ordered against, and moving it would
		// only disturb whatever it currently sits beside.
		return nil
	}

	// Title breaks ties so that two documents claiming the same position come
	// out the same way on every run rather than following map iteration.
	sort.SliceStable(wanted, func(i, j int) bool {
		if wanted[i].Order != wanted[j].Order {
			return wanted[i].Order < wanted[j].Order
		}
		return wanted[i].Title < wanted[j].Title
	})

	children, err := api.GetChildPages(parentID)
	if err != nil {
		return err
	}

	// Where each page sits now, among this parent's children as Confluence
	// lists them -- which is the order the tree is shown in.
	position := make(map[string]int, len(children))
	for i, child := range children {
		position[child.ID] = i
	}

	current := make([]int, 0, len(wanted))
	for _, p := range wanted {
		at, ok := position[p.PageID]
		if !ok {
			// Published under a different parent than the one it is listed
			// under, or created moments ago and not yet visible. Either way
			// there is nothing to compare it against.
			log.Debug().Msgf("page %q is not listed under %s; leaving its position alone", p.Title, parentID)
			return nil
		}
		current = append(current, at)
	}

	// Pages already in the right order relative to each other are the ones
	// worth keeping still; everything else moves. Taking the longest such run
	// is what makes the number of moves minimal.
	keep := longestIncreasingSubsequence(current)
	settled := make(map[int]bool, len(keep))
	for _, i := range keep {
		settled[i] = true
	}

	if len(keep) == len(wanted) {
		return nil
	}

	// Left to right, so that by the time a page is placed after its
	// predecessor, that predecessor is already where it finally belongs.
	for i := range wanted {
		if settled[i] {
			continue
		}

		// The first page has nothing to sit after, so it is placed ahead of the
		// one that follows it instead. Without this a page asked to lead never
		// moved at all.
		before := i == 0
		var neighbour Ordered
		if before {
			neighbour = wanted[1]
		} else {
			neighbour = wanted[i-1]
		}

		where := "after"
		if before {
			where = "before"
		}

		if dryRun {
			log.Info().Msgf("page %q would be moved %s %q", wanted[i].Title, where, neighbour.Title)
			continue
		}

		log.Info().Msgf("moving page %q %s %q", wanted[i].Title, where, neighbour.Title)

		var err error
		if before {
			err = api.MoveContentBefore(wanted[i].PageID, neighbour.PageID)
		} else {
			err = api.MoveContentAfter(wanted[i].PageID, neighbour.PageID)
		}
		if err != nil {
			return fmt.Errorf("unable to order page %q: %w", wanted[i].Title, err)
		}
	}

	return nil
}

// longestIncreasingSubsequence returns the indices of a longest run of values
// that is already in ascending order.
//
// Those are the pages that need not move: every page outside the run has to be
// repositioned no matter what, and no larger set can be left in place, so this
// is the smallest number of moves that produces the requested order.
func longestIncreasingSubsequence(values []int) []int {
	if len(values) == 0 {
		return nil
	}

	// tails[k] is the index of the smallest value that ends an increasing run
	// of length k+1; prev threads each element back to its predecessor.
	tails := []int{}
	prev := make([]int, len(values))
	for i := range prev {
		prev[i] = -1
	}

	for i, v := range values {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if values[tails[mid]] < v {
				lo = mid + 1
			} else {
				hi = mid
			}
		}

		if lo > 0 {
			prev[i] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}

	result := make([]int, len(tails))
	k := tails[len(tails)-1]
	for i := len(tails) - 1; i >= 0; i-- {
		result[i] = k
		k = prev[k]
	}

	return result
}
