package confluence_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCloudRestrictAPI is a Cloud-looking fake with one page to restrict. The
// fake answers /api/v2/spaces, so the client takes the Cloud path.
func newCloudRestrictAPI(t *testing.T) (*confluence.API, *confluencetest.Server, *confluence.PageInfo) {
	t.Helper()
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	stored := server.AddPage("DOCS", "Locked", "page", "")
	server.SetCurrentUser(confluencetest.User{
		AccountID: "acct-me",
		Email:     "me@example.com",
		FullName:  "Mark Bot",
	})
	require.True(t, api.IsCloud(), "the fixture has to look like Cloud for this to mean anything")
	return api, server, &confluence.PageInfo{ID: stored.ID, Title: stored.Title}
}

// TestRestrictPageUpdatesUnknownUserFails pins the bug where a name that
// resolved to nobody quietly restricted the page to the authenticated user
// instead, locking out the person the restriction was meant for.
func TestRestrictPageUpdatesUnknownUserFails(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)

	err := api.RestrictPageUpdates(page, "Nobody Here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"Nobody Here"`, "the user that could not be resolved has to be named")
	assert.Contains(t, err.Error(), page.ID)

	assert.Empty(t, server.Page(page.ID).UpdateRestrictedTo,
		"no restriction may be applied when the named user is unknown")
	assert.Zero(t, server.CountRequests("POST", "/restriction"))
}

// TestRestrictPageUpdatesCurrentUserByEmail is what --edit-lock does on Cloud:
// the configured username is the account's email, which the full-name search
// does not find, and the page is locked to the authenticated account.
func TestRestrictPageUpdatesCurrentUserByEmail(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)

	require.NoError(t, api.RestrictPageUpdates(page, "ME@example.com"))
	assert.Equal(t, []string{"acct-me"}, server.Page(page.ID).UpdateRestrictedTo)
}

// TestRestrictPageUpdatesNamedUser restricts to someone other than the
// authenticated user, found through the user search.
func TestRestrictPageUpdatesNamedUser(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	require.NoError(t, api.RestrictPageUpdates(page, "Alice Example"))
	assert.Equal(t, []string{"acct-alice"}, server.Page(page.ID).UpdateRestrictedTo)
}
