package page

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
)

// TestParentIDForFallsBackToTheResolvedParent pins the class of bug where a
// folder-parented page reports no parent at all. Folders are not pages and
// never appear among a page's expanded ancestors, so anything asking the page
// alone -- page ordering is the one that mattered -- silently got nothing.
func TestParentIDForFallsBackToTheResolvedParent(t *testing.T) {
	folderParented := &confluence.PageInfo{ID: "1", Title: "Child"}
	folder := &confluence.PageInfo{ID: "42", Type: "folder-parent", Title: "Manuals"}

	assert.Empty(t, ImmediateParentID(folderParented),
		"a folder-parented page has no ancestors to read")
	assert.Equal(t, "42", ParentIDFor(folderParented, folder))
}

// TestParentIDForPrefersTheExpandedAncestor guards the ordinary case: what
// Confluence says about where the page actually sits wins over what this run
// resolved, which may since have been moved.
func TestParentIDForPrefersTheExpandedAncestor(t *testing.T) {
	pg := &confluence.PageInfo{
		ID: "1",
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{{ID: "9", Title: "Home"}, {ID: "7", Title: "Parent"}},
	}

	assert.Equal(t, "7", ParentIDFor(pg, &confluence.PageInfo{ID: "42"}))
}

// TestParentIDForKnowsNothing keeps the "" answer available: a page with no
// ancestors and no resolved parent is one nothing can be ordered against, and
// OrderChildren has to be able to tell.
func TestParentIDForKnowsNothing(t *testing.T) {
	assert.Empty(t, ParentIDFor(&confluence.PageInfo{ID: "1"}, nil))
	assert.Empty(t, ParentIDFor(nil, nil))
}
