package confluence_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
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

func TestSetPageRestrictionsCurrentUserByEmail(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	var body []byte
	var readErr error
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/restriction") {
			body, readErr = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		return 0, "", false
	})

	require.NoError(t, api.SetPageRestrictions(page, []confluence.PageRestriction{
		{Operation: "read", Users: []string{"ME@example.com"}},
	}))
	require.NoError(t, readErr)
	require.JSONEq(t, `[{"operation":"read","restrictions":{"user":[{"type":"known","accountId":"acct-me"}],"group":[]}}]`, string(body))
	assert.Zero(t, server.CountRequests(http.MethodGet, "/search"))
}

// newCloudRestrictAPIWithoutCurrentUser is a Cloud fake whose /user/current
// refuses. It is addressed through the gateway, so the client knows it is
// Cloud without asking for the current user and caching the answer.
func newCloudRestrictAPIWithoutCurrentUser(t *testing.T) (*confluence.API, *confluencetest.Server, *confluence.PageInfo) {
	t.Helper()
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	stored := server.AddPage("DOCS", "Locked", "page", "")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasSuffix(r.URL.Path, "/rest/api/user/current") {
			return http.StatusForbidden, `{"message":"forbidden"}`, true
		}
		return 0, "", false
	})
	api := confluence.NewAPI(server.URL+"/ex/confluence/cloud-id", "user", "token", false)
	require.True(t, api.IsCloud())
	return api, server, &confluence.PageInfo{ID: stored.ID, Title: stored.Title}
}

func TestSetPageRestrictionsRefusesUnknownCurrentUser(t *testing.T) {
	api, server, page := newCloudRestrictAPIWithoutCurrentUser(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	err := api.SetPageRestrictions(page, []confluence.PageRestriction{
		{Operation: "read", Users: []string{"Alice Example"}},
	})
	require.ErrorContains(t, err, "could lock it out")
	assert.Zero(t, server.CountRequests(http.MethodPut, "/restriction"))
}

// TestRestrictPageUpdatesNamedUser restricts to someone other than the
// authenticated user, found through the user search.
func TestRestrictPageUpdatesNamedUser(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	require.NoError(t, api.RestrictPageUpdates(page, "Alice Example"))
	assert.Equal(t, []string{"acct-alice"}, server.Page(page.ID).UpdateRestrictedTo)
}

// newCloudLoginAPI is a Cloud fake whose client authenticates as login, and
// whose current-user endpoint answers with email -- empty when the profile
// hides it, or another address when the login is an alias.
func newCloudLoginAPI(t *testing.T, login, email string) (*confluence.API, *confluencetest.Server, *confluence.PageInfo) {
	t.Helper()
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, login, "token", false)
	server.AddSpace("DOCS")
	stored := server.AddPage("DOCS", "Locked", "page", "")
	server.SetCurrentUser(confluencetest.User{
		AccountID: "acct-me",
		Email:     email,
		FullName:  "Mark Bot",
	})
	require.True(t, api.IsCloud(), "the fixture has to look like Cloud for this to mean anything")
	return api, server, &confluence.PageInfo{ID: stored.ID, Title: stored.Title}
}

// TestRestrictPageUpdatesOwnLoginWithoutVisibleEmail is --edit-lock on Cloud
// when the current user's profile does not name the login: the email is
// hidden by the profile-visibility settings, or the login is an alias of
// another address. The login is still the account mark authenticated as, so
// the page is locked to it.
func TestRestrictPageUpdatesOwnLoginWithoutVisibleEmail(t *testing.T) {
	for name, email := range map[string]string{
		"hidden email": "",
		"alias login":  "primary@example.com",
	} {
		t.Run(name, func(t *testing.T) {
			api, server, page := newCloudLoginAPI(t, "login@example.com", email)

			require.NoError(t, api.RestrictPageUpdates(page, "Login@Example.com"))
			assert.Equal(t, []string{"acct-me"}, server.Page(page.ID).UpdateRestrictedTo)
		})
	}
}

// TestRestrictPageUpdatesOtherNameStillRefused keeps the restriction strict for
// anyone but the login: a name that is neither the login nor one the current
// user goes by is not taken to mean the current user.
func TestRestrictPageUpdatesOtherNameStillRefused(t *testing.T) {
	api, server, page := newCloudLoginAPI(t, "login@example.com", "")

	err := api.RestrictPageUpdates(page, "someone-else@example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to restrict the page to anyone else")
	assert.Empty(t, server.Page(page.ID).UpdateRestrictedTo)
	assert.Zero(t, server.CountRequests("POST", "/restriction"))
}

// TestRestrictPageUpdatesTokenWithoutUsername is a Personal Access Token setup:
// with no login, --edit-lock passes an empty name and the page is locked to the
// current user, while any other name is still refused.
func TestRestrictPageUpdatesTokenWithoutUsername(t *testing.T) {
	api, server, page := newCloudLoginAPI(t, "", "")

	require.NoError(t, api.RestrictPageUpdates(page, ""))
	assert.Equal(t, []string{"acct-me"}, server.Page(page.ID).UpdateRestrictedTo)

	err := api.RestrictPageUpdates(page, "someone-else@example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to restrict the page to anyone else")
}

func TestRestrictPageUpdatesNamedUserWhenCurrentUnavailable(t *testing.T) {
	api, server, page := newCloudRestrictAPIWithoutCurrentUser(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	require.NoError(t, api.RestrictPageUpdates(page, "Alice Example"))
	assert.Equal(t, []string{"acct-alice"}, server.Page(page.ID).UpdateRestrictedTo)
}

// TestSetPageRestrictionsReplacesEveryRestriction: the PUT stands for every
// restriction on the page, so an operation it leaves out is lifted, and the
// authenticated user is added to the one it names.
func TestSetPageRestrictionsReplacesEveryRestriction(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})
	require.NoError(t, api.RestrictPageUpdates(page, ""))
	require.Equal(t, []string{"acct-me"}, server.Page(page.ID).UpdateRestrictedTo)

	require.NoError(t, api.SetPageRestrictions(page, []confluence.PageRestriction{
		{Operation: "read", Users: []string{"Alice Example"}, Groups: []string{"docs-readers"}},
	}))

	stored := server.Page(page.ID)
	assert.Equal(t, []string{"acct-alice", "acct-me"}, stored.ReadRestrictedTo)
	assert.Empty(t, stored.UpdateRestrictedTo, "an operation left out is lifted")
	assert.Equal(t, map[string][]string{"read": {"docs-readers"}}, stored.RestrictedGroups)
}

// TestSetPageRestrictionsNamesEachSubjectOnce: a user named twice, or named
// as well as added for being the authenticated user, and a group named twice
// in another case, are each sent once.
func TestSetPageRestrictionsNamesEachSubjectOnce(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	require.NoError(t, api.SetPageRestrictions(page, []confluence.PageRestriction{{
		Operation: "update",
		Users:     []string{"Alice Example", "Alice Example", "me@example.com"},
		Groups:    []string{"Docs", "docs"},
	}}))

	stored := server.Page(page.ID)
	assert.Equal(t, []string{"acct-alice", "acct-me"}, stored.UpdateRestrictedTo)
	assert.Equal(t, map[string][]string{"update": {"Docs"}}, stored.RestrictedGroups)
}

// TestSetPageRestrictionsLiftsWithoutTheCurrentUser: lifting a restriction
// cannot lock anyone out, so it does not need the authenticated user.
func TestSetPageRestrictionsLiftsWithoutTheCurrentUser(t *testing.T) {
	api, server, page := newCloudRestrictAPIWithoutCurrentUser(t)

	require.NoError(t, api.SetPageRestrictions(page, []confluence.PageRestriction{{Operation: "read"}}))
	assert.Empty(t, server.Page(page.ID).ReadRestrictedTo)
	assert.Equal(t, 1, server.CountRequests(http.MethodPut, "/restriction"))
}
