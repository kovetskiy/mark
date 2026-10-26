package confluence_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
			require.NoError(t, api.UpdatePage(created, "<p>body</p>", false, "", "", ""))
			for _, name := range []string{"a.png", "b.png"} {
				_, err = api.CreateAttachment(created.ID, name, "c", strings.NewReader("png"))
				require.NoError(t, err)
			}

			existing := server.AddPage("DOCS", "Existing", "page", "")
			page, err := api.GetPageByID(existing.ID)
			require.NoError(t, err)
			require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))

			assert.NotEmpty(t, server.Watchers(created.ID), "nothing is unwatched before the writes are done")

			api.UnwatchWritten()
			assert.Empty(t, server.Watchers(created.ID), "a page mark created, updated and attached files to")
			assert.Empty(t, server.Watchers(existing.ID), "a page mark found and updated")
			assert.Equal(t, 2, server.CountRequests("DELETE", "/user/watch/content/"), "one unwatch per page")
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
	api.UnwatchWritten()

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
	api.UnwatchWritten()

	assert.Equal(t, []string{"acct-current"}, server.Watchers(created.ID))
	assert.Zero(t, server.CountRequests("GET", "/user/watch/")+server.CountRequests("DELETE", "/user/watch/"))
}

// A Confluence that refuses the watch endpoints, as the gateway does a scoped
// token without classic scopes, is warned about once for reading and once for
// unwatching, not for every page and attachment mark writes.
func TestUnwatchWarnsOnceWhenWatchingIsRefused(t *testing.T) {
	api, server := newUnwatchAPI(t, false)
	server.AddSpace("DOCS")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/user/watch/") {
			return http.StatusForbidden, `{"message":"scope does not match"}`, true
		}
		return 0, "", false
	})

	buffer := captureLog(t)

	created, err := api.CreatePage("DOCS", "page", nil, "New", "")
	require.NoError(t, err)
	for _, name := range []string{"a.png", "b.png"} {
		_, err = api.CreateAttachment(created.ID, name, "c", strings.NewReader("png"))
		require.NoError(t, err)
	}
	existing := server.AddPage("DOCS", "Existing", "page", "")
	page, err := api.GetPageByID(existing.ID)
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))
	api.UnwatchWritten()

	assert.Equal(t, 2, strings.Count(buffer.String(), `"level":"warn"`), buffer.String())
	assert.Contains(t, buffer.String(), "unable to read whether 1 of 2 page(s) mark wrote to were watched before")
	assert.Contains(t, buffer.String(), "unable to stop watching 1 of 1 page(s)")
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buffer)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return &buffer
}

// Every page whose watch state could not be read is named in one warning at
// the end of the run, not only the first.
func TestUnwatchReportsEveryPageItCouldNotRead(t *testing.T) {
	api, server := newUnwatchAPI(t, false)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/user/watch/") {
			return http.StatusForbidden, `{"message":"scope does not match"}`, true
		}
		return 0, "", false
	})
	buffer := captureLog(t)

	var ids []string
	for _, title := range []string{"One", "Two"} {
		stored := server.AddPage("DOCS", title, "page", "")
		page, err := api.GetPageByID(stored.ID)
		require.NoError(t, err)
		require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))
		ids = append(ids, stored.ID)
	}
	api.UnwatchWritten()

	assert.Equal(t, 1, strings.Count(buffer.String(), `"level":"warn"`), buffer.String())
	assert.Contains(t, buffer.String(), "unable to read whether 2 of 2 page(s)")
	for _, id := range ids {
		assert.Contains(t, buffer.String(), id)
		assert.Equal(t, []string{"acct-current"}, server.Watchers(id), "a page whose state is unknown stays watched")
	}
	assert.Zero(t, server.CountRequests("DELETE", "/user/watch/"))
}

// On Data Center without movepage.action a move is an update, which makes the
// user watch the page: the page is recorded before it, so the watch that move
// made is not taken for the user's own.
func TestUnwatchAfterAMoveByUpdate(t *testing.T) {
	api, server := newDataCenterAPIWithoutMoveAction(t)
	server.SetWatchOnWrite()
	api.Unwatch = true

	parent := server.AddPage("DOCS", "Parent", "page", "")
	moved := server.AddPage("DOCS", "Moved and updated", "page", "")
	onlyMoved := server.AddPage("DOCS", "Only moved", "page", "")

	require.NoError(t, api.MoveContentAppend(moved.ID, parent.ID))
	require.NoError(t, api.MoveContentAppend(onlyMoved.ID, parent.ID))
	require.Equal(t, []string{"current"}, server.Watchers(moved.ID), "the move has to be one that makes a watcher")

	page, err := api.GetPageByID(moved.ID)
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))
	api.UnwatchWritten()

	assert.Empty(t, server.Watchers(moved.ID))
	assert.Empty(t, server.Watchers(onlyMoved.ID))
}

// A page mark only labels, as --changes-only leaves one whose content is
// unchanged, is recorded like any other write.
func TestUnwatchAfterLabelWrites(t *testing.T) {
	api, server := newUnwatchAPI(t, false)
	added := server.AddPage("DOCS", "Added", "page", "")
	removed := server.AddPage("DOCS", "Removed", "page", "")

	page, err := api.GetPageByID(added.ID)
	require.NoError(t, err)
	_, err = api.AddPageLabels(page, []string{"docs"})
	require.NoError(t, err)
	page, err = api.GetPageByID(removed.ID)
	require.NoError(t, err)
	_, err = api.DeletePageLabel(page, "docs")
	require.NoError(t, err)
	api.UnwatchWritten()

	assert.Equal(t, 1, server.CountRequests("DELETE", "/user/watch/content/"+added.ID))
	assert.Equal(t, 1, server.CountRequests("DELETE", "/user/watch/content/"+removed.ID))
}
