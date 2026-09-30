package page_test

import (
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderWithTwoPages builds Anchor > Guides (a folder) > First, Second.
func folderWithTwoPages(t *testing.T, server *confluencetest.Server) (*confluencetest.Folder, *confluencetest.Page) {
	t.Helper()

	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	folder := server.AddFolder("DOCS", "Guides", anchor.ID, "page")
	first := server.AddPage("DOCS", "First", "page", folder.ID)
	server.AddPage("DOCS", "Second", "page", folder.ID)

	return folder, first
}

// assertNotMoved checks that relocating a page into the folder it already sits
// in sent no move: the move appends, so it would reshuffle the folder's
// children on every run.
func assertNotMoved(t *testing.T, api *confluence.API, server *confluencetest.Server, pg *confluence.PageInfo, folder *confluencetest.Folder, first *confluencetest.Page) {
	t.Helper()

	server.ResetRequests()
	require.NoError(t, page.EnsurePageUnderFolderParent(api, pg, folder.ID))

	assert.Equal(t, 0, server.CountRequests(http.MethodPut, "/move/"),
		"page %s already sits in folder %s; a move request re-appends it", first.ID, folder.ID)
	assert.Equal(t, first.ID, server.ChildOrder(folder.ID)[0], "order among siblings")
}

// TestAPageAlreadyInItsFolderIsNotMovedAgain: v1 never lists a folder among a
// page's ancestors, so a page found through it looks parentless, and mark
// moved it into the folder it was already in on every run.
func TestAPageAlreadyInItsFolderIsNotMovedAgain(t *testing.T) {
	api, server := newAPI(t)
	require.True(t, api.IsCloud())

	folder, first := folderWithTwoPages(t, server)

	pg, err := api.FindPage("DOCS", "First", "page")
	require.NoError(t, err)

	assertNotMoved(t, api, server, pg, folder, first)
}

// TestAPageAlreadyInItsFolderIsNotMovedAgainThroughTheGateway is the same
// through the api.atlassian.com gateway, where the page is read from v2 and
// its parent is named outright.
func TestAPageAlreadyInItsFolderIsNotMovedAgainThroughTheGateway(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL+"/ex/confluence/cloud-id", "user", "token", false)
	require.True(t, api.IsCloud())

	folder, first := folderWithTwoPages(t, server)

	pg, err := api.FindPage("DOCS", "First", "page")
	require.NoError(t, err)
	require.Equal(t, folder.ID, page.ImmediateParentID(pg), "v2 names the folder as the parent")

	assertNotMoved(t, api, server, pg, folder, first)
}

// TestAPageInAnotherFolderIsStillMoved: the check above must not stop a page
// that sits in some other folder from being moved.
func TestAPageInAnotherFolderIsStillMoved(t *testing.T) {
	api, server := newAPI(t)

	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	from := server.AddFolder("DOCS", "Old", anchor.ID, "page")
	to := server.AddFolder("DOCS", "New", anchor.ID, "page")
	stored := server.AddPage("DOCS", "First", "page", from.ID)

	pg, err := api.FindPage("DOCS", "First", "page")
	require.NoError(t, err)

	require.NoError(t, page.EnsurePageUnderFolderParent(api, pg, to.ID))

	assert.Equal(t, to.ID, server.Page(stored.ID).ParentID)
}
