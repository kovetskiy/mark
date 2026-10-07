package confluence_test

import (
	"encoding/json"
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
// a current user with no accountId, so IsCloud() is false, no /api/v2 at all,
// and no content move endpoint.
// Everything else -- content read, content update, ancestors on an update,
// /pages/movepage.action -- is what those releases really do serve.
func newDataCenterAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()

	return newDataCenterAPIWith(t, nil)
}

// newDataCenterAPIWithoutMoveAction is newDataCenterAPI where movepage.action
// is not reachable either, which leaves a reparent only the update.
func newDataCenterAPIWithoutMoveAction(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()

	return newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			return http.StatusNotFound, `{"message":"no such action"}`, true
		}
		return 0, "", false
	})
}

// newDataCenterAPIWith is newDataCenterAPI with f consulted on every request
// the fixture itself does not refuse.
func newDataCenterAPIWith(
	t *testing.T, f confluencetest.FailFunc,
) (*confluence.API, *confluencetest.Server) {
	t.Helper()

	api, server := newAPI(t)
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		if f != nil {
			return f(r)
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
	api, server := newDataCenterAPIWithoutMoveAction(t)

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
// part of what an update can carry, so with movepage.action unreachable as well
// ordering is the one thing that cannot be done, and has to be named as such.
func TestOrderingOnDataCenterSaysWhatCannotBeDone(t *testing.T) {
	api, server := newDataCenterAPIWithoutMoveAction(t)

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
			assert.Contains(t, err.Error(), "movepage.action")
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
	api, server := newDataCenterAPIWithoutMoveAction(t)

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

// TestReparentOnDataCenterUsesMovePageAction: the action moves a page the way
// the Move dialog does, without writing a version of it.
func TestReparentOnDataCenterUsesMovePageAction(t *testing.T) {
	api, server := newDataCenterAPI(t)

	oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
	newParent := server.AddPage("DOCS", "New Parent", "page", "")
	moved := server.AddPage("DOCS", "Release Notes", "page", oldParent.ID)
	server.EditPage(moved.ID, "<p>the notes</p>")
	before := server.Page(moved.ID).Version

	require.NoError(t, api.MoveContentAppend(moved.ID, newParent.ID))

	after := server.Page(moved.ID)
	assert.Equal(t, newParent.ID, after.ParentID)
	assert.Equal(t, before, after.Version)
	assert.Equal(t, "<p>the notes</p>", after.Body)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/pages/movepage.action"))
	assert.Zero(t, countPuts(server, moved.ID),
		"the update fallback is not needed when the action worked")
}

// TestOrderingOnDataCenterUsesMovePageAction: ordering has no other way round
// outside Cloud, so the action is what makes the Order header work there.
func TestOrderingOnDataCenterUsesMovePageAction(t *testing.T) {
	api, server := newDataCenterAPI(t)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)
	third := server.AddPage("DOCS", "Third", "page", parent.ID)

	require.NoError(t, api.MoveContentBefore(third.ID, first.ID))
	assert.Equal(t, []string{third.ID, first.ID, second.ID}, server.ChildOrder(parent.ID))

	require.NoError(t, api.MoveContentAfter(third.ID, second.ID))
	assert.Equal(t, []string{first.ID, second.ID, third.ID}, server.ChildOrder(parent.ID))
}

// TestMovePageActionRefusalIsReported: a 200 carrying actionErrors is a
// refusal, which a reparent works around with an update and an ordering
// reports in the action's own words.
func TestMovePageActionRefusalIsReported(t *testing.T) {
	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			return http.StatusOK, `{"actionErrors":["You cannot move this page."]}`, true
		}
		return 0, "", false
	})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)
	other := server.AddPage("DOCS", "Other", "page", "")

	err := api.MoveContentBefore(second.ID, first.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "You cannot move this page.")
	assert.Equal(t, []string{first.ID, second.ID}, server.ChildOrder(parent.ID))

	require.NoError(t, api.MoveContentAppend(second.ID, other.ID))
	assert.Equal(t, other.ID, server.Page(second.ID).ParentID)
	assert.Equal(t, 1, countPuts(server, second.ID),
		"the reparent has to have been done by update")
}

// TestMovePageActionBehindALoginPageIsGivenUp: an action that turns these
// credentials away with Seraph's login page does so for every page, so it is
// not asked again for the rest of the run.
func TestMovePageActionBehindALoginPageIsGivenUp(t *testing.T) {
	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			return http.StatusOK, `<html><body>Log in</body></html>`, true
		}
		return 0, "", false
	})
	server.SetFailHeaders(http.Header{
		"Content-Type":         {"text/html"},
		"X-Seraph-Loginreason": {"AUTHENTICATED_FAILED"},
	})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", "")
	second := server.AddPage("DOCS", "Second", "page", "")

	require.NoError(t, api.MoveContentAppend(first.ID, parent.ID))
	require.NoError(t, api.MoveContentAppend(second.ID, parent.ID))

	assert.Equal(t, parent.ID, server.Page(first.ID).ParentID)
	assert.Equal(t, parent.ID, server.Page(second.ID).ParentID)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/pages/movepage.action"))
}

// TestMovePageActionVersionIsNotAnEdit: should the action write a version of
// the page, that version is mark's, and --no-overwrite must be able to tell.
func TestMovePageActionVersionIsNotAnEdit(t *testing.T) {
	var (
		server *confluencetest.Server
		pageID string
	)

	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			server.EditPage(pageID, "<p>as moved</p>")
		}
		return 0, "", false
	})

	oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
	newParent := server.AddPage("DOCS", "New Parent", "page", "")
	pageID = server.AddPage("DOCS", "Release Notes", "page", oldParent.ID).ID
	before := server.Page(pageID).Version

	require.NoError(t, api.MoveContentAppend(pageID, newParent.ID))

	from, moved := api.ReparentedFrom(pageID, server.Page(pageID).Version)
	require.True(t, moved)
	assert.Equal(t, before, from)
}

// countPuts counts updates of the content itself, which a move endpoint
// request under the same path is not.
func countPuts(server *confluencetest.Server, id string) int {
	n := 0
	for _, r := range server.Requests() {
		if r.Method == http.MethodPut && strings.HasSuffix(r.Path, "/rest/api/content/"+id) {
			n++
		}
	}
	return n
}

// TestMovePageActionThatSettlesLateIsNotRedone: Data Center can answer the
// action before the REST view shows the new parent. A claimed success is read
// back again rather than taken for a no-op and redone with an update.
func TestMovePageActionThatSettlesLateIsNotRedone(t *testing.T) {
	var (
		server        *confluencetest.Server
		pageID, newID string
		pending       bool
		reads         int
	)

	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		switch {
		case r.URL.Path == "/pages/movepage.action":
			pending = true
			return http.StatusOK, `{"page":{"id":"` + pageID + `"}}`, true
		case pending && r.Method == http.MethodGet && r.URL.Path == "/rest/api/content/"+pageID:
			reads++
			if reads == 3 {
				server.MovePage(pageID, newID)
			}
		}
		return 0, "", false
	})

	oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
	newID = server.AddPage("DOCS", "New Parent", "page", "").ID
	pageID = server.AddPage("DOCS", "Release Notes", "page", oldParent.ID).ID
	before := server.Page(pageID).Version

	require.NoError(t, api.MoveContentAppend(pageID, newID))

	assert.Equal(t, newID, server.Page(pageID).ParentID)
	assert.Equal(t, before, server.Page(pageID).Version)
	assert.Zero(t, countPuts(server, pageID), "the move must not be redone by update")
}

// TestMovePageActionAnsweringHTMLAfterAMoveIsTrusted: the action may answer a
// move that worked with a page rather than JSON -- the page it moved, after a
// redirect -- and Data Center may not show the move in the REST view yet. The
// tree is read back as for any answer that is not a refusal, rather than once,
// and the action stays in use for the next page.
func TestMovePageActionAnsweringHTMLAfterAMoveIsTrusted(t *testing.T) {
	var (
		server          *confluencetest.Server
		pending, target string
		reads           int
	)

	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		switch {
		case r.URL.Path == "/pages/movepage.action":
			pending, reads = r.URL.Query().Get("pageId"), 0
			return http.StatusOK, `<html><body>Release Notes</body></html>`, true
		case pending != "" && r.Method == http.MethodGet && r.URL.Path == "/rest/api/content/"+pending:
			reads++
			if reads == 3 {
				server.MovePage(pending, target)
				pending = ""
			}
		}
		return 0, "", false
	})
	server.SetFailHeaders(http.Header{"Content-Type": {"text/html"}})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	target = parent.ID
	first := server.AddPage("DOCS", "First", "page", "")
	second := server.AddPage("DOCS", "Second", "page", "")

	require.NoError(t, api.MoveContentAppend(first.ID, parent.ID))
	require.NoError(t, api.MoveContentAppend(second.ID, parent.ID))

	assert.Equal(t, parent.ID, server.Page(first.ID).ParentID)
	assert.Equal(t, parent.ID, server.Page(second.ID).ParentID)
	assert.Equal(t, 2, server.CountRequests(http.MethodPost, "/pages/movepage.action"),
		"an HTML answer that is not a login page must not give the action up")
	assert.Zero(t, countPuts(server, first.ID)+countPuts(server, second.ID),
		"a move the action did is not redone by update")
}

// TestMovePageActionHTMLRefusalOfOnePageIsNotGivenUp: an HTML answer that
// left one page where it was -- a restricted page, a proxy's error for that
// request -- is about that page. The next is still moved with the action, and
// ordering on the rest of the run still works.
func TestMovePageActionHTMLRefusalOfOnePageIsNotGivenUp(t *testing.T) {
	var (
		server     *confluencetest.Server
		restricted string
	)

	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" && r.URL.Query().Get("pageId") == restricted {
			return http.StatusOK, `<html><body>Not permitted</body></html>`, true
		}
		return 0, "", false
	})
	server.SetFailHeaders(http.Header{"Content-Type": {"text/html"}})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)
	restricted = server.AddPage("DOCS", "Restricted", "page", "").ID

	require.NoError(t, api.MoveContentAppend(restricted, parent.ID))
	assert.Equal(t, parent.ID, server.Page(restricted).ParentID)
	assert.Equal(t, 1, countPuts(server, restricted), "that one page is reparented by update")

	require.NoError(t, api.MoveContentBefore(second.ID, first.ID))
	assert.Equal(t, second.ID, server.ChildOrder(parent.ID)[0],
		"ordering has to go on working through the action")
}

// TestMovePageActionFieldErrorsAreARefusal: XWork reports a refusal under
// fieldErrors too, keyed by field. Read as success, it was waited on and then
// reported without the reason Confluence gave.
func TestMovePageActionFieldErrorsAreARefusal(t *testing.T) {
	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			return http.StatusOK, `{"fieldErrors":{"targetId":["The target is not a valid parent."]}}`, true
		}
		return 0, "", false
	})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	err := api.MoveContentBefore(second.ID, first.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targetId: The target is not a valid parent.")
}

// TestActionErrorsReadsEveryShape covers the forms XWork puts an error in.
func TestActionErrorsReadsEveryShape(t *testing.T) {
	for name, testcase := range map[string]struct {
		answer string
		want   string
	}{
		"action errors":         {`{"actionErrors":["no"]}`, ": no"},
		"field errors":          {`{"fieldErrors":{"b":["second"],"a":"first"}}`, ": a: first; b: second"},
		"validation errors map": {`{"validationErrors":{"title":"taken"}}`, ": title: taken"},
		"error message":         {`{"errorMessage":"denied"}`, ": denied"},
		"empty lists":           {`{"actionErrors":[],"fieldErrors":{}}`, ""},
		"a success":             {`{"page":{"id":"1"}}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var answer map[string]any
			require.NoError(t, json.Unmarshal([]byte(testcase.answer), &answer))
			assert.Equal(t, testcase.want, confluence.ActionErrors(answer))
		})
	}
}
