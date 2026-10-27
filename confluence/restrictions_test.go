package confluence_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restrict publishes declared restrictions on page the way a run does, around
// a content update that does nothing here, and says what the interim step
// left on the page.
func restrict(
	t *testing.T, api *confluence.API, server *confluencetest.Server, page *confluence.PageInfo,
	lockUpdates bool, declared ...confluence.PageRestriction,
) (*confluence.RestrictionPlan, *confluencetest.Page) {
	t.Helper()

	resolved, err := api.ResolveRestrictions(declared)
	require.NoError(t, err)
	plan, err := api.PlanRestrictions(page, resolved, lockUpdates, false)
	require.NoError(t, err)

	require.NoError(t, api.ApplyInterim(plan))
	interim := server.Page(page.ID)
	require.NoError(t, api.ApplyFinal(plan))

	return plan, interim
}

func TestRestrictionsCurrentUserByEmail(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)

	restrict(t, api, server, page, false, confluence.PageRestriction{Operation: "read", Users: []string{"ME@example.com"}})

	assert.Equal(t, []string{"acct-me"}, server.Page(page.ID).ReadRestrictedTo)
	assert.Zero(t, server.CountRequests(http.MethodGet, "/search"), "the authenticated user needs no search")
}

// TestRestrictionsRefuseWithoutTheCurrentUser: a restriction that names
// anyone could lock mark out unless it adds itself, so it is refused when
// mark cannot tell who it is -- before anything is written.
func TestRestrictionsRefuseWithoutTheCurrentUser(t *testing.T) {
	api, server, _ := newCloudRestrictAPIWithoutCurrentUser(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	_, err := api.ResolveRestrictions([]confluence.PageRestriction{{Operation: "read", Users: []string{"Alice Example"}}})
	require.ErrorContains(t, err, "could lock it out")
	assert.Zero(t, server.CountRequests(http.MethodPut, "/restriction"))
}

// TestRestrictionsLiftWithoutTheCurrentUser: lifting a restriction cannot
// lock anyone out, so it does not need the authenticated user.
func TestRestrictionsLiftWithoutTheCurrentUser(t *testing.T) {
	api, server, page := newCloudRestrictAPIWithoutCurrentUser(t)
	server.SetRestrictions(page.ID, "read", []string{"acct-alice"}, nil)

	restrict(t, api, server, page, false, confluence.PageRestriction{Operation: "read"})
	assert.Empty(t, server.Page(page.ID).ReadRestrictedTo)
}

// TestRestrictionsKeepWhatTheDocumentDoesNotName: an operation the document
// leaves out keeps the restriction somebody set in the UI.
func TestRestrictionsKeepWhatTheDocumentDoesNotName(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})
	server.SetRestrictions(page.ID, "read", []string{"acct-me"}, []string{"hr"})

	restrict(t, api, server, page, false, confluence.PageRestriction{Operation: "update", Users: []string{"Alice Example"}})

	stored := server.Page(page.ID)
	assert.Equal(t, []string{"acct-me"}, stored.ReadRestrictedTo)
	assert.Equal(t, []string{"hr"}, stored.RestrictedGroups["read"], "read is left as the UI set it")
	assert.Equal(t, []string{"acct-alice", "acct-me"}, stored.UpdateRestrictedTo)
}

// TestRestrictionsNameEachSubjectOnce: a user named twice, or named as well
// as added for being the authenticated user, and a group named twice in
// another case, are each sent once.
func TestRestrictionsNameEachSubjectOnce(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})

	restrict(t, api, server, page, false, confluence.PageRestriction{
		Operation: "update",
		Users:     []string{"Alice Example", "alice example", "me@example.com"},
		Groups:    []string{"Docs", "docs"},
	})

	stored := server.Page(page.ID)
	assert.Equal(t, []string{"acct-alice", "acct-me"}, stored.UpdateRestrictedTo)
	assert.Equal(t, []string{"Docs"}, stored.RestrictedGroups["update"])
}

// TestRestrictionsMatchANameExactly: the user search matches any full name
// containing the one asked for, and access is not given to the first of
// them. Two people of the same name have to be told apart by accountId.
func TestRestrictionsMatchANameExactly(t *testing.T) {
	api, server, _ := newCloudRestrictAPI(t)
	server.AddUser(confluencetest.User{AccountID: "acct-smith", FullName: "Jane Doe-Smith"})
	server.AddUser(confluencetest.User{AccountID: "acct-jane", FullName: "Jane Doe"})

	resolve := func(name string) error {
		_, err := api.ResolveRestrictions([]confluence.PageRestriction{{Operation: "read", Users: []string{name}}})
		return err
	}

	require.NoError(t, resolve("Jane Doe"))
	require.ErrorContains(t, resolve("Jane"), "no user has exactly that name")

	server.AddUser(confluencetest.User{AccountID: "acct-jane-2", FullName: "Jane Doe"})
	err := resolve("Jane Doe")
	require.ErrorContains(t, err, "ambiguous")
	assert.ErrorContains(t, err, "accountid:acct-jane-2")

	require.NoError(t, resolve("accountId:acct-jane-2"))
}

// TestRestrictionsTightenBeforeAndLoosenAfter: before the content update the
// page holds whichever of its current and its final restrictions allows
// fewer people.
func TestRestrictionsTightenBeforeAndLoosenAfter(t *testing.T) {
	read := func(groups ...string) confluence.PageRestriction {
		return confluence.PageRestriction{Operation: "read", Groups: groups}
	}

	t.Run("tightening happens before", func(t *testing.T) {
		api, server, page := newCloudRestrictAPI(t)
		_, interim := restrict(t, api, server, page, false, read("hr"))
		assert.Equal(t, []string{"hr"}, interim.RestrictedGroups["read"])
		assert.Equal(t, []string{"acct-me"}, interim.ReadRestrictedTo)
	})

	t.Run("lifting happens after", func(t *testing.T) {
		api, server, page := newCloudRestrictAPI(t)
		server.SetRestrictions(page.ID, "read", []string{"acct-me"}, []string{"hr"})
		_, interim := restrict(t, api, server, page, false, read())
		assert.Equal(t, []string{"hr"}, interim.RestrictedGroups["read"], "still restricted while the content is written")
		assert.Empty(t, server.Page(page.ID).RestrictedGroups["read"])
	})

	t.Run("a swap holds both in between", func(t *testing.T) {
		api, server, page := newCloudRestrictAPI(t)
		server.SetRestrictions(page.ID, "read", []string{"acct-me"}, []string{"hr", "staff"})
		_, interim := restrict(t, api, server, page, false, read("staff", "eng"))
		assert.Equal(t, []string{"staff"}, interim.RestrictedGroups["read"], "only those both allow")
		assert.Equal(t, []string{"acct-me"}, interim.ReadRestrictedTo, "and mark")
		assert.Equal(t, []string{"eng", "staff"}, server.Page(page.ID).RestrictedGroups["read"])
	})

	t.Run("an interim updater can read", func(t *testing.T) {
		api, server, page := newCloudRestrictAPI(t)
		server.AddUser(confluencetest.User{AccountID: "acct-carol", FullName: "Carol"})
		server.SetRestrictions(page.ID, "read", []string{"acct-alice", "acct-me"}, nil)
		_, interim := restrict(t, api, server, page, false,
			confluence.PageRestriction{Operation: "read"},
			confluence.PageRestriction{Operation: "update", Users: []string{"Carol"}},
		)
		assert.Equal(t, []string{"acct-alice", "acct-me"}, interim.ReadRestrictedTo)
		assert.Equal(t, []string{"acct-me"}, interim.UpdateRestrictedTo, "Carol cannot read until read is lifted")
		assert.Equal(t, []string{"acct-carol", "acct-me"}, server.Page(page.ID).UpdateRestrictedTo)
	})
}

// TestRestrictionsUnchangedSendNothing: a page that already has what its
// document declares is not written, however often it is published.
func TestRestrictionsUnchangedSendNothing(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.SetRestrictions(page.ID, "read", []string{"acct-me"}, []string{"HR"})

	plan, _ := restrict(t, api, server, page, false, confluence.PageRestriction{Operation: "read", Groups: []string{"hr"}})
	assert.False(t, plan.Changes())
	assert.Zero(t, server.CountRequests(http.MethodPut, "/restriction"))
}

// TestRestrictionsKeepTheEditLock: under --edit-lock the authenticated user
// stays among the updaters, so the replacing PUT never lifts the lock, even
// for an update the document lifts.
func TestRestrictionsKeepTheEditLock(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	require.NoError(t, api.RestrictPageUpdates(page, ""))

	_, interim := restrict(t, api, server, page, true,
		confluence.PageRestriction{Operation: "update"},
		confluence.PageRestriction{Operation: "read", Groups: []string{"hr"}},
	)
	assert.Equal(t, []string{"acct-me"}, interim.UpdateRestrictedTo)
	assert.Equal(t, []string{"acct-me"}, server.Page(page.ID).UpdateRestrictedTo)
}

// TestRestrictionsRefuseAPartialList: a restriction list that may have been
// cut short is not compared against, since writing back what is missing
// from it would lift restrictions nobody asked to lift.
func TestRestrictionsRefuseAPartialList(t *testing.T) {
	api, server, page := newCloudRestrictAPI(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasSuffix(r.URL.Path, "/restriction/byOperation") {
			return http.StatusOK, `{"read":{"restrictions":{"user":{"results":[{"accountId":"acct-a"}],"limit":1},"group":{"results":[]}}}}`, true
		}
		return 0, "", false
	})

	resolved, err := api.ResolveRestrictions([]confluence.PageRestriction{{Operation: "read", Groups: []string{"hr"}}})
	require.NoError(t, err)
	_, err = api.PlanRestrictions(page, resolved, false, false)
	require.ErrorContains(t, err, "more read restrictions than one request returns")
	assert.Zero(t, server.CountRequests(http.MethodPut, "/restriction"))
}
