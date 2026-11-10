package page_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleOrphansKeepsAFolderParentWhenThePlatformIsUnidentified: a Cloud site
// on a custom domain has only its current user to tell it is Cloud, and a proxy
// answering that request with a 502 leaves it unidentified for the whole run. The
// folder check that guards the deletion must not read that as "no folders
// here": the page would be trashed, and the folders and everything in them
// with it.
func TestHandleOrphansKeepsAFolderParentWhenThePlatformIsUnidentified(t *testing.T) {
	ctx := t.Context()
	api, server := newAPI(t)
	gone := server.AddPage("DOCS", "Gone", "page", "")
	folder := server.AddFolder("DOCS", "Manuals", gone.ID, "page")
	server.AddFolder("DOCS", "Nested", folder.ID, "folder")

	// Retry-After: 0 lets retryTransport run out of retries at once.
	server.SetFailHeaders(http.Header{"Retry-After": []string{"0"}})
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return http.StatusBadGateway, `{"message":"bad gateway"}`, true
		}
		return 0, "", false
	})
	_, cloudErr := api.Cloud(context.Background())
	require.Error(t, cloudErr, "the platform has to go unidentified for this to mean anything")

	handled, err := page.HandleOrphans(ctx, api, report.OnOrphanDelete, []page.Orphan{
		{Path: "gone.md", PageID: gone.ID, Title: "Gone"},
	}, false)
	require.NoError(t, err)
	assert.Empty(t, handled, "a page left alone stays tracked")

	trashed := server.Page(gone.ID)
	require.NotNil(t, trashed)
	assert.False(t, trashed.Trashed, "a page holding folders must be left alone")
}

// TestHandleOrphansKeepsAFolderParentBehindAnSSOProxy: a Cloud site behind a
// corporate SSO proxy can answer the current user with an HTML sign-in page and
// a 200. That identifies nothing, so it does not rule Cloud out, and the folder
// check still runs before the page is trashed.
func TestHandleOrphansKeepsAFolderParentBehindAnSSOProxy(t *testing.T) {
	ctx := t.Context()
	api, server := newAPI(t)
	gone := server.AddPage("DOCS", "Gone", "page", "")
	server.AddFolder("DOCS", "Manuals", gone.ID, "page")

	server.SetFailHeaders(http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return http.StatusOK, "<!doctype html><html><body>Sign in with SSO</body></html>", true
		}
		return 0, "", false
	})
	_, cloudErr := api.Cloud(context.Background())
	require.Error(t, cloudErr, "the platform has to go unidentified for this to mean anything")

	handled, err := page.HandleOrphans(ctx, api, report.OnOrphanDelete, []page.Orphan{
		{Path: "gone.md", PageID: gone.ID, Title: "Gone"},
	}, false)
	require.NoError(t, err)
	assert.Empty(t, handled, "a page left alone stays tracked")

	trashed := server.Page(gone.ID)
	require.NotNil(t, trashed)
	assert.False(t, trashed.Trashed, "a page holding folders must be left alone")
}

// TestHandleOrphansTakesAPageAlreadyGoneAsHandled: a page somebody removed by
// hand is where --on-orphan wanted it. Failing over it instead would fail every
// later run the same way, since the page stays tracked.
func TestHandleOrphansTakesAPageAlreadyGoneAsHandled(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive} {
		for _, how := range []string{"trashed", "purged"} {
			t.Run(string(action)+"/"+how, func(t *testing.T) {
				ctx := t.Context()
				api, server := newAPI(t)
				gone := server.AddPage("DOCS", "Gone", "page", "")
				if how == "trashed" {
					require.NoError(t, api.DeletePage(ctx, gone.ID))
				} else {
					server.DeletePage(gone.ID)
				}

				handled, err := page.HandleOrphans(ctx, api, action, []page.Orphan{
					{Path: "gone.md", PageID: gone.ID, Title: "Gone"},
				}, false)
				require.NoError(t, err)
				assert.Equal(t, []string{"gone.md"}, handled)
			})
		}
	}
}

// TestHandleOrphansKeepsAPageThatIsStillThere: a 404 from the child listing
// alone does not make a page gone. Taking it as gone would forget a live page.
func TestHandleOrphansKeepsAPageThatIsStillThere(t *testing.T) {
	ctx := t.Context()
	api, server := newAPI(t)
	live := server.AddPage("DOCS", "Live", "page", "")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/content/"+live.ID+"/child/page" {
			return http.StatusNotFound, `{"message":"not here"}`, true
		}
		return 0, "", false
	})

	handled, err := page.HandleOrphans(ctx, api, report.OnOrphanDelete, []page.Orphan{
		{Path: "live.md", PageID: live.ID, Title: "Live"},
	}, false)
	require.Error(t, err)
	assert.Empty(t, handled)
	assert.False(t, server.Page(live.ID).Trashed)
}
