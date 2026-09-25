package confluence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These live inside the package so that the wait before a retry can be taken
// out; everything else goes over real HTTP to the fake, which answers a PUT
// naming anything but the next version with 409, as Confluence does.

func newConflictAPI(t *testing.T, gateway bool) (*API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)

	base := server.URL
	if gateway {
		base += "/ex/confluence/cloud-id"
	}

	api := NewAPI(base, "user", "token", false)
	api.conflictRetryDelay = 0
	return api, server
}

// failWhen installs a fail hook that also keeps v1 out of reach on the
// gateway, since a scoped token is not entitled to it.
func failWhen(server *confluencetest.Server, gateway bool, f confluencetest.FailFunc) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if gateway && strings.Contains(r.URL.Path, "/rest/api/") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		if f != nil {
			return f(r)
		}
		return 0, "", false
	})
}

// isContentPut reports whether r is the page update itself, on either API.
func isContentPut(r *http.Request, id string) bool {
	return r.Method == http.MethodPut &&
		(strings.HasSuffix(r.URL.Path, "/rest/api/content/"+id) ||
			strings.HasSuffix(r.URL.Path, "/api/v2/pages/"+id) ||
			strings.HasSuffix(r.URL.Path, "/api/v2/blogposts/"+id))
}

func countContentPuts(server *confluencetest.Server, id string) int {
	n := 0
	for _, r := range server.Requests() {
		if r.Method == http.MethodPut &&
			(strings.HasSuffix(r.Path, "/content/"+id) ||
				strings.HasSuffix(r.Path, "/pages/"+id) ||
				strings.HasSuffix(r.Path, "/blogposts/"+id)) {
			n++
		}
	}
	return n
}

// A page written after mark read it -- by hand, or by mark itself through a
// second copy -- used to fail the publish with a bare 409. The update is now
// sent again against the version the page has, and mark's content wins.
func TestUpdatePageRetriesAStaleVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gateway  bool
		pageType string
	}{
		{"v1", false, "page"},
		{"gateway", true, "page"},
		{"gateway blogpost", true, "blogpost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, server := newConflictAPI(t, tc.gateway)
			failWhen(server, tc.gateway, nil)
			stored := server.AddPage("DOCS", "Target", tc.pageType, "")

			page, err := api.FindPage("DOCS", "Target", tc.pageType)
			require.NoError(t, err)
			require.NotNil(t, page)
			require.EqualValues(t, 1, page.Version.Number)

			server.EditPage(stored.ID, "<p>by hand</p>")
			server.EditPage(stored.ID, "<p>by hand, twice</p>")
			server.ResetRequests()

			require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "sync", "full-width", ""))

			after := server.Page(stored.ID)
			assert.Equal(t, "<p>from mark</p>", after.Body)
			assert.EqualValues(t, 4, after.Version)
			assert.EqualValues(t, 4, page.Version.Number,
				"the caller has to hold the version that was actually written")
			assert.Equal(t, 2, countContentPuts(server, stored.ID), "one retry, no more")

			cached, err := api.FindPage("DOCS", "Target", tc.pageType)
			require.NoError(t, err)
			require.NotNil(t, cached)
			assert.EqualValues(t, 4, cached.Version.Number,
				"a stale cache would make the next update of the page conflict again")

			// And the next update goes straight through.
			server.ResetRequests()
			require.NoError(t, api.UpdatePage(page, "<p>again</p>", false, "", "full-width", ""))
			assert.Equal(t, 1, countContentPuts(server, stored.ID))
		})
	}
}

// v1 carries the properties inside the update, so the retried PUT has to carry
// them as well; the refused one never took effect.
func TestUpdatePageRetryCarriesPropertiesV1(t *testing.T) {
	api, server := newConflictAPI(t, false)
	stored := server.AddPage("DOCS", "Target", "page", "")

	page, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	server.EditPage(stored.ID, "<p>by hand</p>")

	var bodies [][]byte
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, stored.ID) {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			bodies = append(bodies, body)
		}
		return 0, "", false
	})

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "full-width", "🚀"))
	require.Len(t, bodies, 2)

	var retried struct {
		Version struct {
			Number int64 `json:"number"`
		} `json:"version"`
		Metadata struct {
			Properties map[string]struct {
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(bodies[1], &retried))
	assert.EqualValues(t, 3, retried.Version.Number)
	assert.Equal(t, "full-width", retried.Metadata.Properties["content-appearance-published"].Value)
	assert.Equal(t, "1f680", retried.Metadata.Properties["emoji-title-published"].Value)
}

// v2 writes the properties once the content is in, so they go on after the
// retried PUT rather than being lost with the refused one.
func TestUpdatePageRetryWritesPropertiesV2(t *testing.T) {
	api, server := newConflictAPI(t, true)
	failWhen(server, true, nil)
	stored := server.AddPage("DOCS", "Target", "page", "")

	page, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	server.EditPage(stored.ID, "<p>by hand</p>")

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "full-width", "🚀"))

	appearance := server.SpaceProperty(stored.ID, "content-appearance-published")
	require.NotNil(t, appearance)
	assert.JSONEq(t, `"full-width"`, string(appearance.Value))

	emoji := server.SpaceProperty(stored.ID, "emoji-title-published")
	require.NotNil(t, emoji)
	assert.JSONEq(t, `"1f680"`, string(emoji.Value))
}

// Issue #139: Confluence Cloud has refused the first update of a page created
// a moment earlier with an OptimisticLockException, although the version sent
// was right. The page has not moved on, so the retry sends the same number.
func TestUpdatePageRetriesATransientConflict(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(map[bool]string{false: "overwrite", true: "keep concurrent edits"}[keep], func(t *testing.T) {
			api, server := newConflictAPI(t, false)
			api.KeepConcurrentEdits = keep
			server.AddSpace("DOCS")

			page, err := api.CreatePage("DOCS", "page", nil, "Fresh", "")
			require.NoError(t, err)

			refused := false
			failWhen(server, false, func(r *http.Request) (int, string, bool) {
				if isContentPut(r, page.ID) && !refused {
					refused = true
					return http.StatusConflict, `{"statusCode":409,"message":"com.atlassian.confluence.api.service.exceptions.ConflictException: javax.persistence.OptimisticLockException: Row was updated or deleted by another transaction"}`, true
				}
				return 0, "", false
			})

			require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "full-width", ""))
			assert.Equal(t, "<p>body</p>", server.Page(page.ID).Body)
			assert.EqualValues(t, 2, server.Page(page.ID).Version)
			assert.EqualValues(t, 2, page.Version.Number)
		})
	}
}

// A page that conflicts on the retry as well is being written continuously by
// something else. Racing it is not a fix, so mark stops and says so.
func TestUpdatePagePersistentConflictFails(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "gateway"}[gateway], func(t *testing.T) {
			api, server := newConflictAPI(t, gateway)
			failWhen(server, gateway, nil)
			stored := server.AddPage("DOCS", "Busy", "page", "")

			page, err := api.GetPageByID(stored.ID)
			require.NoError(t, err)

			// Every update is overtaken by another writer just before it lands.
			failWhen(server, gateway, func(r *http.Request) (int, string, bool) {
				if isContentPut(r, stored.ID) {
					server.EditPage(stored.ID, "<p>the other writer</p>")
				}
				return 0, "", false
			})

			err = api.UpdatePage(page, "<p>from mark</p>", false, "", "full-width", "")
			require.Error(t, err)
			assert.True(t, errors.Is(err, errConflict))
			assert.Contains(t, err.Error(), `page "Busy"`)
			assert.Contains(t, err.Error(), "again after reading the page afresh")
			assert.Equal(t, 2, countContentPuts(server, stored.ID), "exactly one retry")
			assert.Equal(t, "<p>the other writer</p>", server.Page(stored.ID).Body)
			assert.EqualValues(t, 1, page.Version.Number,
				"nothing was written, so the caller's version must not move")
		})
	}
}

// Under --no-overwrite an edit landing between the drift check and the update
// is exactly what the flag protects, so the conflict is not retried over it.
func TestUpdatePageKeepConcurrentEditsRefusesToOverwrite(t *testing.T) {
	api, server := newConflictAPI(t, false)
	api.KeepConcurrentEdits = true
	stored := server.AddPage("DOCS", "Guarded", "page", "")

	page, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	server.EditPage(stored.ID, "<p>by hand</p>")

	err = api.UpdatePage(page, "<p>from mark</p>", false, "", "full-width", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errConflict))
	assert.Contains(t, err.Error(), "edited in Confluence while mark was publishing it")
	assert.Equal(t, "<p>by hand</p>", server.Page(stored.ID).Body)
	assert.Equal(t, 1, countContentPuts(server, stored.ID))
}

// newConflictDataCenterAPI is newConflictAPI dressed as Server or Data Center,
// so that a reparent takes the update fallback: no /api/v2, and no content
// move endpoint. f sees every other request.
func newConflictDataCenterAPI(
	t *testing.T, f confluencetest.FailFunc,
) (*API, *confluencetest.Server) {
	t.Helper()

	api, server := newConflictAPI(t, false)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") || strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return f(r)
	})
	require.False(t, api.IsCloud())

	return api, server
}

// A reparent on Server/DC writes the page's body back, and it is the body it
// read. Somebody saving the page between that read and the write used to have
// their edit reverted: the retry re-read only the version number and sent the
// old body as the version after theirs. A move is not allowed to cost a page
// its content, and a concurrent edit is content -- with or without
// --no-overwrite, since a move rebuilt from a fresh read overwrites nothing.
func TestReparentConflictKeepsTheConcurrentEdit(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("KeepConcurrentEdits=%v", keep), func(t *testing.T) {
			var (
				server *confluencetest.Server
				pageID string
				edited bool
			)

			api, server := newConflictDataCenterAPI(t, func(r *http.Request) (int, string, bool) {
				if !edited && isContentPut(r, pageID) {
					edited = true
					server.EditPage(pageID, "<p>edited meanwhile</p>")
				}
				return 0, "", false
			})
			api.KeepConcurrentEdits = keep

			oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
			newParent := server.AddPage("DOCS", "New Parent", "page", "")
			pageID = server.AddPage("DOCS", "Release Notes", "page", oldParent.ID).ID
			server.EditPage(pageID, "<p>as mark read it</p>")

			require.NoError(t, api.MoveContentAppend(pageID, newParent.ID))

			require.True(t, edited, "the edit has to land between the read and the write")
			after := server.Page(pageID)
			assert.Equal(t, newParent.ID, after.ParentID)
			assert.Equal(t, "<p>edited meanwhile</p>", after.Body,
				"the move must not put back the body it read before the edit")
			assert.Equal(t, "edited by hand", after.Message,
				"nor the version message, which is where --changes-only looks")
			assert.Equal(t, 2, countContentPuts(server, pageID))
		})
	}
}

// The retry is bounded the way UpdatePage's is: a page that conflicts on the
// fresh read as well is being written continuously, and the error says so.
func TestReparentPersistentConflictFails(t *testing.T) {
	var (
		server *confluencetest.Server
		pageID string
	)

	api, server := newConflictDataCenterAPI(t, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, pageID) {
			server.EditPage(pageID, "<p>edited again</p>")
		}
		return 0, "", false
	})

	oldParent := server.AddPage("DOCS", "Old Parent", "page", "")
	newParent := server.AddPage("DOCS", "New Parent", "page", "")
	pageID = server.AddPage("DOCS", "Release Notes", "page", oldParent.ID).ID

	err := api.MoveContentAppend(pageID, newParent.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errConflict))
	assert.Contains(t, err.Error(), "something else is writing to the page")
	assert.Equal(t, 2, countContentPuts(server, pageID), "exactly one retry")
	assert.Equal(t, oldParent.ID, server.Page(pageID).ParentID)
}
