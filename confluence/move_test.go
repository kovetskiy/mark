package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMoveOnServerSaysTheEndpointIsCloudOnly pins the class of bug where a
// Cloud-only endpoint is called with no capability check at all.
//
// content/{id}/move is reached whenever a page's ancestry stops matching its
// headers, and whenever pages are ordered. Folder creation is gated on
// IsCloud() and RestrictPageUpdates detects the old-Server case and says so;
// this path did neither, so a Server or Data Center user reorganising a docs
// tree got a bare 404 and nothing to act on.
func TestMoveOnServerSaysTheEndpointIsCloudOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confluence Server: no /api/v2 at all, and no content move endpoint.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no such endpoint"}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)
	require.False(t, api.IsCloud(), "the fixture has to look like Server for this to mean anything")

	for name, move := range map[string]func() error{
		"append": func() error { return api.MoveContentAppend("1004", "1003") },
		"before": func() error { return api.MoveContentBefore("1004", "1005") },
		"after":  func() error { return api.MoveContentAfter("1004", "1005") },
	} {
		t.Run(name, func(t *testing.T) {
			err := move()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not Cloud")
			assert.Contains(t, err.Error(), "1004", "the page being moved has to be named")
			assert.Contains(t, err.Error(), "404", "and the status it was refused with")
		})
	}
}

// TestMoveNamesThePageByTitleWhenItCanUses the page cache rather than a second
// request: the call is only reached once something has already gone wrong.
func TestMoveNamesThePageByTitleWhenItCan(t *testing.T) {
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

	err = api.MoveContentAppend(page.ID, "1003")
	require.Error(t, err)
	require.True(t, moveRefused)
	assert.Contains(t, err.Error(), `"Release Notes"`,
		"a person reading this has to know which page could not be moved")
}

// TestMoveOnCloudStillReportsAPlain404: the capability message must not swallow
// a 404 that really is a missing page, which on Cloud is what one means.
func TestMoveOnCloudStillReportsAPlain404(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	require.True(t, api.IsCloud(), "the fake answers /api/v2/spaces, so it is Cloud")

	err := api.MoveContentAppend("no-such-page", "1003")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NotContains(t, err.Error(), "not Cloud")
}
