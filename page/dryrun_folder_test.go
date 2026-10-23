package page_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDryRunDoesNotMoveAFolder covers the one write a dry run was still making.
//
// A folder left at the space root by an earlier sync is moved under the page
// --parents names. That happens while the ancestry is being worked out, which
// is long before the guard deciding whether this run writes anything -- so the
// mode that promises to write nothing reparented a folder, and every page
// inside it went with it.
func TestDryRunDoesNotMoveAFolder(t *testing.T) {
	ctx := t.Context()
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "Anchor", "page", "")

	// At the space root, where an earlier sync left it: no page or folder
	// parent, which is what makes it a candidate for the move.
	server.AddFolder("DOCS", "Manuals", "", "")

	var moved atomic.Bool

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			moved.Store(true)
		}

		return 0, "", false
	})

	_, err := page.EnsureFolderAncestry(ctx, true, api, "DOCS", []string{"Manuals"}, &anchor.ID, nil)
	require.NoError(t, err)

	assert.False(t, moved.Load(), "a dry run must not move anything")

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Empty(t, folders[0].ParentID, "the folder must still be where it was")
}

// TestARealRunStillAsksToMoveTheFolder is the control: the move is wanted, and
// only the mode that writes nothing should be without it.
//
// The request is what is asserted rather than the folder's new parent, because
// the fake Confluence moves pages and not folders -- so the move is observed
// going out, and whatever it answers is beside the point here.
func TestARealRunStillAsksToMoveTheFolder(t *testing.T) {
	ctx := t.Context()
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	server.AddFolder("DOCS", "Manuals", "", "")

	var moved atomic.Bool

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			moved.Store(true)
		}

		return 0, "", false
	})

	_, _ = page.EnsureFolderAncestry(ctx, false, api, "DOCS", []string{"Manuals"}, &anchor.ID, nil)

	assert.True(t, moved.Load(), "a real run asks to move the folder")
}

// TestDryRunMixedAncestryIgnoresCaseOfAnchorTitle: a MARK_PARENTS chain that
// differs from the existing anchor page only in case names that page, so a dry
// run finds the folders beneath it rather than standing in for ones a real run
// would create.
func TestDryRunMixedAncestryIgnoresCaseOfAnchorTitle(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "My Parent", "page", "")
	folder := server.AddFolder("DOCS", "Manuals", anchor.ID, "page")

	parent, err := page.EnsureMixedAncestry(t.Context(), true, api, nil, "DOCS", []string{"Manuals"}, []string{"my parent"})
	require.NoError(t, err)
	require.NotNil(t, parent)

	assert.Equal(t, folder.ID, parent.ID, "the existing folder under the anchor is the parent")
}
