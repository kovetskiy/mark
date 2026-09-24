package page_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dataCenterAPI is the fake dressed as Confluence Server or Data Center: no
// /api/v2, and no content move endpoint. Those two absences are what a
// self-hosted instance looks like to mark.
func dataCenterAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()

	api, server := newAPI(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") || strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})
	require.False(t, api.IsCloud())

	return api, server
}

// TestEnsurePageUnderParentOnDataCenter: a page whose headers name a parent it
// does not sit under is moved there on a self-hosted instance, where there is
// no move endpoint and the move is an update carrying the new ancestor.
func TestEnsurePageUnderParentOnDataCenter(t *testing.T) {
	api, server := dataCenterAPI(t)

	old := server.AddPage("DOCS", "Old Parent", "page", "")
	want := server.AddPage("DOCS", "New Parent", "page", "")
	stored := server.AddPage("DOCS", "Release Notes", "page", old.ID)
	server.EditPage(stored.ID, "<p>the notes</p>")

	pg, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)

	require.NoError(t, page.EnsurePageUnderParent(api, pg, want.ID))

	assert.Equal(t, want.ID, server.Page(stored.ID).ParentID)
	assert.Equal(t, "<p>the notes</p>", server.Page(stored.ID).Body)
	assert.Equal(t, want.ID, page.ImmediateParentID(pg),
		"the caller's page has to carry the ancestry the move produced")
}

// TestUpdateAfterMoveOnDataCenterIsNotAConflict pins the part a caller cannot
// see: the move writes a new version of the page, and mark publishes the
// document a moment later against the version it holds. A move that left that
// number behind would have every relocated page refused as a conflict.
func TestUpdateAfterMoveOnDataCenterIsNotAConflict(t *testing.T) {
	api, server := dataCenterAPI(t)

	old := server.AddPage("DOCS", "Old Parent", "page", "")
	want := server.AddPage("DOCS", "New Parent", "page", "")
	stored := server.AddPage("DOCS", "Release Notes", "page", old.ID)

	pg, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	require.NoError(t, page.EnsurePageUnderParent(api, pg, want.ID))

	require.NoError(t, api.UpdatePage(pg, "<p>published</p>", false, "", "full-width", ""))

	after := server.Page(stored.ID)
	assert.Equal(t, "<p>published</p>", after.Body)
	assert.Equal(t, want.ID, after.ParentID, "publishing must not undo the move")
}
