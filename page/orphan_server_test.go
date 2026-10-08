package page_test

import (
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleOrphansKeepsAFolderParentWhenThePlatformIsUnidentified: a Cloud site
// on a custom domain has only its current user to tell it is Cloud, and a proxy
// answering that request with a 502 makes IsCloud false for the whole run. The
// folder check that guards the deletion must not read that as "no folders
// here": the page would be trashed, and the folders and everything in them
// with it.
func TestHandleOrphansKeepsAFolderParentWhenThePlatformIsUnidentified(t *testing.T) {
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
	require.False(t, api.IsCloud(), "the platform has to go unidentified for this to mean anything")

	handled, err := page.HandleOrphans(api, page.OnOrphanDelete, []page.Orphan{
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
	require.False(t, api.IsCloud(), "the platform has to go unidentified for this to mean anything")

	handled, err := page.HandleOrphans(api, page.OnOrphanDelete, []page.Orphan{
		{Path: "gone.md", PageID: gone.ID, Title: "Gone"},
	}, false)
	require.NoError(t, err)
	assert.Empty(t, handled, "a page left alone stays tracked")

	trashed := server.Page(gone.ID)
	require.NotNil(t, trashed)
	assert.False(t, trashed.Trashed, "a page holding folders must be left alone")
}
