package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDataCenterAPI is the fake dressed as Confluence Server or Data Center:
// no /api/v2 at all, so IsCloud() is false, and no content move endpoint.
// Everything else -- content read, content update, ancestors on an update --
// is what those releases really do serve.
func newDataCenterAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()

	api, server := newAPI(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") || strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})
	require.False(t, api.IsCloud(), "the fixture has to look like Data Center for this to mean anything")

	return api, server
}

// TestReparentOnDataCenterFallsBackToAnUpdate is the whole point of the
// fallback: mark works out that a page belongs somewhere else and then puts it
// there, on an instance with no move endpoint to call.
//
// It used to stop at the working out. The page's ancestry was compared against
// its headers, the move was attempted, Confluence answered 404, and the run
// failed telling the user to drag the page across by hand.
func TestReparentOnDataCenterFallsBackToAnUpdate(t *testing.T) {
	api, server := newDataCenterAPI(t)

	oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
	newParent := server.AddPage("DOCS", "New Parent", "page", "")
	moved := server.AddPage("DOCS", "Release Notes", "page", oldParent.ID)
	server.EditPage(moved.ID, "<p>the notes</p>")

	require.NoError(t, api.MoveContentAppend(moved.ID, newParent.ID))

	after := server.Page(moved.ID)
	require.NotNil(t, after)
	assert.Equal(t, newParent.ID, after.ParentID, "the page has to end up under the new parent")
	assert.Equal(t, "<p>the notes</p>", after.Body,
		"a move must not cost the page its content")
	assert.Equal(t, "Release Notes", after.Title)
	assert.Contains(t, server.ChildOrder(newParent.ID), moved.ID)
}

// TestReparentOnDataCenterStopsProbingTheMissingEndpoint: a run reorganising a
// tree moves many pages, and the endpoint does not come back between two of
// them.
func TestReparentOnDataCenterStopsProbingTheMissingEndpoint(t *testing.T) {
	api, server := newDataCenterAPI(t)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", "")
	second := server.AddPage("DOCS", "Second", "page", "")

	require.NoError(t, api.MoveContentAppend(first.ID, parent.ID))
	require.Equal(t, 1, server.CountRequests(http.MethodPut, "/move/"))

	require.NoError(t, api.MoveContentAppend(second.ID, parent.ID))
	assert.Equal(t, 1, server.CountRequests(http.MethodPut, "/move/"),
		"the second move must not pay for a request that is known to 404")
	assert.Equal(t, parent.ID, server.Page(second.ID).ParentID)
}

// TestReparentOnDataCenterLeavesAPageThatIsAlreadyThereAlone: the fallback
// costs a version every time it writes, and a page under the parent it is
// being moved to has nothing to write.
func TestReparentOnDataCenterLeavesAPageThatIsAlreadyThereAlone(t *testing.T) {
	api, server := newDataCenterAPI(t)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	before := server.Page(child.ID).Version

	require.NoError(t, api.MoveContentAppend(child.ID, parent.ID))

	assert.Equal(t, before, server.Page(child.ID).Version,
		"a move to where the page already is must not bump its version")
}

// TestOrderingOnDataCenterSaysWhatCannotBeDone: position among siblings is not
// part of what an update can carry, so the one thing the missing endpoint
// really costs has to be named as such -- and distinguished from reparenting,
// which now works.
func TestOrderingOnDataCenterSaysWhatCannotBeDone(t *testing.T) {
	api, server := newDataCenterAPI(t)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	for name, order := range map[string]func() error{
		"before": func() error { return api.MoveContentBefore(second.ID, first.ID) },
		"after":  func() error { return api.MoveContentAfter(second.ID, first.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			err := order()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "ordering pages")
			assert.Contains(t, err.Error(), "404", "the status it was refused with")
			assert.Contains(t, err.Error(), second.ID, "the page being ordered has to be named")
		})
	}
}

// TestOrderingNamesThePageByTitleWhenItCan uses the page cache rather than a
// second request: the call is only reached once something has already gone
// wrong.
func TestOrderingNamesThePageByTitleWhenItCan(t *testing.T) {
	var moveRefused bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/move/") {
			moveRefused = true
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"method not allowed"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no v2 here"}`))
			return
		}
		_, _ = w.Write([]byte(
			`{"results":[{"id":"1004","title":"Release Notes","type":"page",` +
				`"status":"current","version":{"number":1}}],"_links":{"base":"/wiki"}}`,
		))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	page, err := api.FindPage("DOCS", "Release Notes", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	err = api.MoveContentAfter(page.ID, "1003")
	require.Error(t, err)
	require.True(t, moveRefused)
	assert.Contains(t, err.Error(), `"Release Notes"`,
		"a person reading this has to know which page could not be placed")
}

// TestMoveOnCloudStillReportsAPlain404: the capability path must not swallow a
// 404 that really is a missing page, which on Cloud is what one means.
func TestMoveOnCloudStillReportsAPlain404(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	require.True(t, api.IsCloud(), "the fake answers /api/v2/spaces, so it is Cloud")

	err := api.MoveContentAppend("no-such-page", "1003")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NotContains(t, err.Error(), "move endpoint")
}

// TestReparentOnDataCenterReportsAMissingPage: the fallback is reached on a
// 404 that really is a missing page too, and what it says then has to be about
// the page rather than about the endpoint.
func TestReparentOnDataCenterReportsAMissingPage(t *testing.T) {
	api, server := newDataCenterAPI(t)
	server.AddPage("DOCS", "Parent", "page", "")

	err := api.MoveContentAppend("no-such-page", "1003")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-page")
	assert.Contains(t, err.Error(), "404")
}

// TestReparentOnDataCenterKeepsTheVersionMessage pins what a move must not
// disturb: mark stores the fingerprint --changes-only compares against in the
// version message, and the caller re-reads the page from the server as soon as
// the move returns. A move that wrote a message of its own erased that
// fingerprint, and every moved page was then republished in full -- the
// failure --changes-only exists to prevent, triggered by the one operation
// that changes no content at all.
func TestReparentOnDataCenterKeepsTheVersionMessage(t *testing.T) {
	api, server := newDataCenterAPI(t)

	old := server.AddPage("DOCS", "Old Parent", "page", "")
	want := server.AddPage("DOCS", "New Parent", "page", "")
	moved := server.AddPage("DOCS", "Release Notes", "page", old.ID)

	const published = "[v0000000000000000000000000000000000000001] published by mark"
	require.NoError(t, api.UpdatePage(
		&confluence.PageInfo{
			ID: moved.ID, Title: "Release Notes", Type: "page",
			Version: struct {
				Number  int64  `json:"number"`
				Message string `json:"message"`
			}{Number: server.Page(moved.ID).Version},
		},
		"<p>the notes</p>", false, published, "full-width", "",
	))
	require.Equal(t, published, server.Page(moved.ID).Message)

	require.NoError(t, api.MoveContentAppend(moved.ID, want.ID))

	after := server.Page(moved.ID)
	assert.Equal(t, want.ID, after.ParentID)
	assert.Equal(t, published, after.Message,
		"the content fingerprint has to survive a move, which changes no content")
	assert.Equal(t, "<p>the notes</p>", after.Body)
}
