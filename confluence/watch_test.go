package confluence_test

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchCloud(t *testing.T) {
	server := confluencetest.New(t)
	server.AddUser(confluencetest.User{AccountID: "acct-jane", FullName: "Jane Doe"})
	server.AddUser(confluencetest.User{AccountID: "acct-jane2", FullName: "Jane Doe-Smith"})
	p := server.AddPage("DOCS", "Page", "page", "")
	api := confluence.NewAPI(server.URL, "user", "token", false)

	require.NoError(t, api.WatchPage(p.ID, ""))
	require.NoError(t, api.WatchPage(p.ID, "Jane Doe"))
	assert.Equal(t, []string{"acct-current", "acct-jane"}, server.Watchers(p.ID),
		"a full name that another contains still names one user")

	watching, err := api.IsWatching(p.ID, "accountid:acct-jane")
	require.NoError(t, err)
	assert.True(t, watching)

	require.NoError(t, api.WatchPage(p.ID, "Current User"))
	assert.Zero(t, server.CountRequests("POST", "accountId=acct-current"),
		"naming yourself is naming nobody")

	require.NoError(t, api.UnwatchPage(p.ID, ""))
	assert.Equal(t, []string{"acct-jane"}, server.Watchers(p.ID))

	require.ErrorContains(t, api.WatchPage(p.ID, "Nobody"), `watcher "Nobody"`)
}

func TestWatchDataCenter(t *testing.T) {
	server := confluencetest.New(t)
	server.SetDataCenter()
	p := server.AddPage("DOCS", "Page", "page", "")
	api := confluence.NewAPI(server.URL, "user", "token", false)

	require.NoError(t, api.WatchPage(p.ID, ""))
	require.NoError(t, api.WatchPage(p.ID, "jane"))
	assert.Equal(t, []string{"current", "jane"}, server.Watchers(p.ID))

	require.NoError(t, api.UnwatchPage(p.ID, "jane"))
	assert.Equal(t, []string{"current"}, server.Watchers(p.ID))
}

func newUnwatchAPI(t *testing.T, dataCenter bool) (*confluence.API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	if dataCenter {
		server.SetDataCenter()
	}
	server.SetWatchOnWrite()
	api := confluence.NewAPI(server.URL, "user", "token", false)
	api.Unwatch = true
	return api, server
}

func TestUnwatchAfterWrites(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dataCenter bool
	}{
		{"cloud", false},
		{"data center", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, server := newUnwatchAPI(t, tc.dataCenter)
			server.AddSpace("DOCS")

			created, err := api.CreatePage("DOCS", "page", nil, "New", "")
			require.NoError(t, err)
			assert.Empty(t, server.Watchers(created.ID), "a page mark created")

			require.NoError(t, api.UpdatePage(created, "<p>body</p>", false, "", "", ""))
			assert.Empty(t, server.Watchers(created.ID), "a page mark updated")

			_, err = api.CreateAttachment(created.ID, "a.png", "c", strings.NewReader("png"))
			require.NoError(t, err)
			assert.Empty(t, server.Watchers(created.ID), "a page mark attached a file to")

			existing := server.AddPage("DOCS", "Existing", "page", "")
			page, err := api.GetPageByID(existing.ID)
			require.NoError(t, err)
			require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))
			assert.Empty(t, server.Watchers(existing.ID), "a page mark found and updated")
		})
	}
}

// Somebody who watched a page on purpose keeps watching it, however many
// times mark writes to it.
func TestUnwatchLeavesAWatchedPageWatched(t *testing.T) {
	api, server := newUnwatchAPI(t, false)
	stored := server.AddPage("DOCS", "Watched", "page", "")

	plain := confluence.NewAPI(server.URL, "user", "token", false)
	require.NoError(t, plain.WatchPage(stored.ID, ""))

	page, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(page, "<p>one</p>", false, "", "", ""))
	_, err = api.CreateAttachment(stored.ID, "a.png", "c", strings.NewReader("png"))
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(page, "<p>two</p>", false, "", "", ""))

	assert.Equal(t, []string{"acct-current"}, server.Watchers(stored.ID))
	assert.Equal(t, 1, server.CountRequests("GET", "/user/watch/content/"+stored.ID),
		"asked once, before the first write")
	assert.Zero(t, server.CountRequests("DELETE", "/user/watch/content/"))
}

func TestWithoutUnwatchPagesStayWatched(t *testing.T) {
	api, server := newUnwatchAPI(t, false)
	api.Unwatch = false
	server.AddSpace("DOCS")

	created, err := api.CreatePage("DOCS", "page", nil, "New", "")
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(created, "<p>body</p>", false, "", "", ""))

	assert.Equal(t, []string{"acct-current"}, server.Watchers(created.ID))
	assert.Zero(t, server.CountRequests("GET", "/user/watch/")+server.CountRequests("DELETE", "/user/watch/"))
}
