package confluence_test

import (
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unidentified is a fake whose platform cannot be told: reading the current
// user fails with status. Everything else, folders included, works.
func unidentified(t *testing.T, status int) *confluencetest.Server {
	t.Helper()

	server := confluencetest.New(t)
	// Retry-After: 0 lets retryTransport run out of retries at once.
	server.SetFailHeaders(http.Header{"Retry-After": []string{"0"}})
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return status, `{"message":"upstream hiccup"}`, true
		}
		return 0, "", false
	})

	return server
}

// failures are answers to the current user that say nothing about the
// platform: one retryTransport retries, one it does not.
var failures = map[string]int{
	"bad gateway":  http.StatusBadGateway,
	"unauthorized": http.StatusUnauthorized,
}

// TestHasChildFoldersOutsideCloudAsksNothing: folders are a Cloud feature, and
// a Data Center instance does not route the v2 children listing at all. Asking
// it anyway failed every orphan removal (--on-orphan delete/archive) with a
// JSON error, so outside Cloud the answer is "no folders" without a request.
func TestHasChildFoldersOutsideCloudAsksNothing(t *testing.T) {
	server := confluencetest.New(t)
	server.SetDataCenter()
	page := server.AddPage("DOCS", "Gone", "page", "")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	require.False(t, api.IsCloud(), "the fixture has to be identified as Data Center for this to mean anything")

	has, err := api.HasChildFolders(page.ID)
	require.NoError(t, err)
	assert.False(t, has)
	assert.Zero(t, server.CountRequests("GET", "/direct-children"),
		"the v2 children route is not asked outside Cloud")
}

// TestFindChildFolderOutsideCloudAsksNothing is the same for the lookup the
// ancestry walk makes (page/ancestry.go, resolveFolder): it reads the same v2
// children route, and outside Cloud there is no folder for it to find.
func TestFindChildFolderOutsideCloudAsksNothing(t *testing.T) {
	server := confluencetest.New(t)
	server.SetDataCenter()
	page := server.AddPage("DOCS", "Parent", "page", "")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	require.False(t, api.IsCloud(), "the fixture has to be identified as Data Center for this to mean anything")

	folder, err := api.FindChildFolder(page.ID, "page", "Manuals")
	require.NoError(t, err)
	assert.Nil(t, folder)
	assert.Zero(t, server.CountRequests("GET", "/direct-children"),
		"the v2 children route is not asked outside Cloud")
}

// TestHasChildFoldersAsksWhenThePlatformIsUnidentified: IsCloud reads a target
// it could not identify as "not Cloud", and a Cloud site on a custom domain has
// nothing else to go on. Skipping the request on that answer told --on-orphan
// delete that a page holding folders was childless, and trashing it took the
// folders with it. Only a target identified as Server or Data Center may skip
// the request.
func TestHasChildFoldersAsksWhenThePlatformIsUnidentified(t *testing.T) {
	for name, status := range failures {
		t.Run(name, func(t *testing.T) {
			server := unidentified(t, status)
			page := server.AddPage("DOCS", "Gone", "page", "")
			server.AddFolder("DOCS", "Manuals", page.ID, "page")

			api := confluence.NewAPI(server.URL, "user", "token", false)
			require.False(t, api.IsCloud(), "the platform has to go unidentified for this to mean anything")

			has, err := api.HasChildFolders(page.ID)
			require.NoError(t, err)
			assert.True(t, has, "an unidentified platform is not evidence that there are no folders")
		})
	}
}

// TestFindChildFolderAsksWhenThePlatformIsUnidentified: as with
// HasChildFolders, a target that could not be identified is not evidence that
// there are no folders.
func TestFindChildFolderAsksWhenThePlatformIsUnidentified(t *testing.T) {
	server := unidentified(t, http.StatusBadGateway)
	page := server.AddPage("DOCS", "Parent", "page", "")
	server.AddFolder("DOCS", "Manuals", page.ID, "page")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	require.False(t, api.IsCloud(), "the platform has to go unidentified for this to mean anything")

	folder, err := api.FindChildFolder(page.ID, "page", "Manuals")
	require.NoError(t, err)
	require.NotNil(t, folder, "the folder beneath the page is found")
	assert.Equal(t, "Manuals", folder.Title)
}
