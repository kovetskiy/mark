package confluence_test

import (
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
