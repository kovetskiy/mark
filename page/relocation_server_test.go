package page_test

import (
	"context"
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
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})
	require.False(t, api.IsCloud(context.Background()))

	return api, server
}

// TestEnsurePageUnderParentOnDataCenter: a page whose headers name a parent it
// does not sit under is moved there on a self-hosted instance, where there is
// no move endpoint and the move is an update carrying the new ancestor.
func TestEnsurePageUnderParentOnDataCenter(t *testing.T) {
	ctx := t.Context()
	api, server := dataCenterAPI(t)

	old := server.AddPage("DOCS", "Old Parent", "page", "")
	want := server.AddPage("DOCS", "New Parent", "page", "")
	stored := server.AddPage("DOCS", "Release Notes", "page", old.ID)
	server.EditPage(stored.ID, "<p>the notes</p>")

	pg, err := api.GetPageByID(context.Background(), stored.ID)
	require.NoError(t, err)

	require.NoError(t, page.EnsurePageUnderParent(ctx, api, pg, want.ID))

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
	ctx := t.Context()
	api, server := dataCenterAPI(t)

	old := server.AddPage("DOCS", "Old Parent", "page", "")
	want := server.AddPage("DOCS", "New Parent", "page", "")
	stored := server.AddPage("DOCS", "Release Notes", "page", old.ID)

	pg, err := api.GetPageByID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.NoError(t, page.EnsurePageUnderParent(ctx, api, pg, want.ID))

	require.NoError(t, api.UpdatePage(context.Background(), pg, "<p>published</p>", false, "", "full-width", ""))

	after := server.Page(stored.ID)
	assert.Equal(t, "<p>published</p>", after.Body)
	assert.Equal(t, want.ID, after.ParentID, "publishing must not undo the move")
}

// folderFixture is a page in a folder, as v1 reads it: with the folder missing
// from its ancestors and no parent id, so only v2 can say where it sits.
func folderFixture(t *testing.T, api *confluence.API, server *confluencetest.Server, inFolder bool) (*confluence.PageInfo, *confluence.PageInfo) {
	t.Helper()

	home := server.AddPage("DOCS", "Home", "page", "")
	folder := server.AddFolder("DOCS", "Manuals", home.ID, "page")
	other := server.AddFolder("DOCS", "Elsewhere", home.ID, "page")

	holder := other.ID
	if inFolder {
		holder = folder.ID
	}
	stored := server.AddPage("DOCS", "Doc", "page", holder)

	pg := &confluence.PageInfo{
		ID: stored.ID, Title: "Doc", Type: "page",
		Ancestors: ancestors(home.ID, "Home"),
	}
	target := &confluence.PageInfo{ID: folder.ID, Title: "Manuals", Type: "folder-parent"}

	return pg, target
}

func v2Reads(server *confluencetest.Server) int {
	n := 0
	for _, r := range server.Requests() {
		if r.Method == http.MethodGet && strings.HasPrefix(r.Path, "/api/v2/") {
			n++
		}
	}

	return n
}

func writes(server *confluencetest.Server) int {
	n := 0
	for _, r := range server.Requests() {
		if r.Method != http.MethodGet {
			n++
		}
	}

	return n
}

// TestWouldMoveAsksV2WhereAFolderChildSits: v1 cannot show a folder parent, so
// whether the page is in the folder is only known by asking v2.
func TestWouldMoveAsksV2WhereAFolderChildSits(t *testing.T) {
	ctx := t.Context()
	t.Run("already in the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		server.ResetRequests()

		assert.False(t, page.WouldMove(ctx, api, pg, target, nil, false))
		assert.NotZero(t, v2Reads(server))
	})

	t.Run("in another folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, false)

		assert.True(t, page.WouldMove(ctx, api, pg, target, nil, false))
	})

	t.Run("v2 cannot say", func(t *testing.T) {
		api, server := dataCenterAPI(t)
		pg, target := folderFixture(t, api, server, true)

		assert.True(t, page.WouldMove(ctx, api, pg, target, nil, false),
			"not knowing counts as a move, as it does for a real run")
	})

	t.Run("ancestors already end at the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		pg.Ancestors = ancestors("home", "Home", target.ID, target.Title)
		server.ResetRequests()

		assert.False(t, page.WouldMove(ctx, api, pg, target, nil, false))
		assert.Zero(t, v2Reads(server), "nothing to ask when v1 already shows the folder")
	})

	t.Run("page id already names the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		pg.ParentID = target.ID
		server.ResetRequests()

		assert.False(t, page.WouldMove(ctx, api, pg, target, nil, false))
		assert.Zero(t, v2Reads(server))
	})
}

// TestEnsurePageUnderFolderParentMovesOnlyWhenNeeded is the real-run half of
// the same question, which WouldMove has to answer the way it does.
func TestEnsurePageUnderFolderParentMovesOnlyWhenNeeded(t *testing.T) {
	ctx := t.Context()
	t.Run("already in the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		server.ResetRequests()

		require.NoError(t, page.EnsurePageUnderFolderParent(ctx, api, pg, target.ID))
		assert.Zero(t, writes(server), "a page already in its folder is not moved again")
	})

	t.Run("in another folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, false)
		server.ResetRequests()

		require.NoError(t, page.EnsurePageUnderFolderParent(ctx, api, pg, target.ID))
		assert.NotZero(t, writes(server))
	})
}

// ancestors builds a v1 ancestor list from id, title pairs.
func ancestors(pairs ...string) []struct {
	ID    string `json:"id"`
	Title string `json:"title"`
} {
	var out []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{pairs[i], pairs[i+1]})
	}

	return out
}

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
func assertNotMoved(ctx context.Context, t *testing.T, api *confluence.API, server *confluencetest.Server, pg *confluence.PageInfo, folder *confluencetest.Folder, first *confluencetest.Page) {
	t.Helper()

	server.ResetRequests()
	require.NoError(t, page.EnsurePageUnderFolderParent(ctx, api, pg, folder.ID))

	assert.Equal(t, 0, server.CountRequests(http.MethodPut, "/move/"),
		"page %s already sits in folder %s; a move request re-appends it", first.ID, folder.ID)
	assert.Equal(t, first.ID, server.ChildOrder(folder.ID)[0], "order among siblings")
}

// TestAPageAlreadyInItsFolderIsNotMovedAgain: v1 never lists a folder among a
// page's ancestors, so a page found through it looks parentless, and mark
// moved it into the folder it was already in on every run.
func TestAPageAlreadyInItsFolderIsNotMovedAgain(t *testing.T) {
	ctx := t.Context()
	api, server := newAPI(t)
	require.True(t, api.IsCloud(context.Background()))

	folder, first := folderWithTwoPages(t, server)

	pg, err := api.FindPage(context.Background(), "DOCS", "First", "page")
	require.NoError(t, err)

	assertNotMoved(ctx, t, api, server, pg, folder, first)
}

// TestAPageAlreadyInItsFolderIsNotMovedAgainThroughTheGateway is the same
// through the api.atlassian.com gateway, where the page is read from v2 and
// its parent is named outright.
func TestAPageAlreadyInItsFolderIsNotMovedAgainThroughTheGateway(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL+"/ex/confluence/cloud-id", "user", "token", false)
	require.True(t, api.IsCloud(context.Background()))

	folder, first := folderWithTwoPages(t, server)

	pg, err := api.FindPage(context.Background(), "DOCS", "First", "page")
	require.NoError(t, err)
	require.Equal(t, folder.ID, page.ImmediateParentID(pg), "v2 names the folder as the parent")

	assertNotMoved(ctx, t, api, server, pg, folder, first)
}

// TestAPageInAnotherFolderIsStillMoved: the check above must not stop a page
// that sits in some other folder from being moved.
func TestAPageInAnotherFolderIsStillMoved(t *testing.T) {
	ctx := t.Context()
	api, server := newAPI(t)

	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	from := server.AddFolder("DOCS", "Old", anchor.ID, "page")
	to := server.AddFolder("DOCS", "New", anchor.ID, "page")
	stored := server.AddPage("DOCS", "First", "page", from.ID)

	pg, err := api.FindPage(context.Background(), "DOCS", "First", "page")
	require.NoError(t, err)

	require.NoError(t, page.EnsurePageUnderFolderParent(ctx, api, pg, to.ID))

	assert.Equal(t, to.ID, server.Page(stored.ID).ParentID)
}
