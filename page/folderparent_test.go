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
	t.Run("already in the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		server.ResetRequests()

		assert.False(t, page.WouldMove(api, pg, target, nil, false))
		assert.NotZero(t, v2Reads(server))
	})

	t.Run("in another folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, false)

		assert.True(t, page.WouldMove(api, pg, target, nil, false))
	})

	t.Run("v2 cannot say", func(t *testing.T) {
		api, server := dataCenterAPI(t)
		pg, target := folderFixture(t, api, server, true)

		assert.True(t, page.WouldMove(api, pg, target, nil, false),
			"not knowing counts as a move, as it does for a real run")
	})

	t.Run("ancestors already end at the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		pg.Ancestors = ancestors("home", "Home", target.ID, target.Title)
		server.ResetRequests()

		assert.False(t, page.WouldMove(api, pg, target, nil, false))
		assert.Zero(t, v2Reads(server), "nothing to ask when v1 already shows the folder")
	})

	t.Run("page id already names the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		pg.ParentID = target.ID
		server.ResetRequests()

		assert.False(t, page.WouldMove(api, pg, target, nil, false))
		assert.Zero(t, v2Reads(server))
	})
}

// TestEnsurePageUnderFolderParentMovesOnlyWhenNeeded is the real-run half of
// the same question, which WouldMove has to answer the way it does.
func TestEnsurePageUnderFolderParentMovesOnlyWhenNeeded(t *testing.T) {
	t.Run("already in the folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, true)
		server.ResetRequests()

		require.NoError(t, page.EnsurePageUnderFolderParent(api, pg, target.ID))
		assert.Zero(t, writes(server), "a page already in its folder is not moved again")
	})

	t.Run("in another folder", func(t *testing.T) {
		api, server := newAPI(t)
		pg, target := folderFixture(t, api, server, false)
		server.ResetRequests()

		require.NoError(t, page.EnsurePageUnderFolderParent(api, pg, target.ID))
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
