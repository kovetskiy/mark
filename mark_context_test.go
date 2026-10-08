package mark

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunContextStopsBetweenFiles: Run is an exported entry point, so a library
// caller can be publishing hundreds of documents with no way to ask it to stop.
// A run cancelled before it starts publishes nothing at all.
func TestRunContextStopsBetweenFiles(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunContext(ctx, publishConfig(server.URL, filepath.Join(dir, "*.md")))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "First"),
		"a run cancelled before it starts publishes nothing")
}

// TestRunContextPublishesWhenNotCancelled is the control.
func TestRunContextPublishesWhenNotCancelled(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Published"))

	require.NoError(t, RunContext(context.Background(), publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Published"))
}

// TestProcessFileContextIsCancellable covers the single-file entry point, which
// a caller loops over itself.
func TestProcessFileContextIsCancellable(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Not Published"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ProcessFileContext(ctx, file, api, publishConfig(server.URL, file))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "Not Published"))
}

// TestRunStillWorksWithoutAContext: the old entry points keep their signatures,
// since they are what every existing caller uses.
func TestRunStillWorksWithoutAContext(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Plain Run"))

	require.NoError(t, Run(publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Plain Run"))
}

// TestRunContextStopsARunBlockedOnASlowRequest: cancellation reaches the
// request in flight. The fake holds the second file's first lookup until the
// client gives up on it, as a Confluence that has stopped answering would, and
// cancelling the run's context ends the run there rather than after the
// two-minute response-header timeout. The page that lookup was for is never
// created, the one before it stands, and the page manifest is still saved on
// the way out, under a context of its own: the second run proves it by finding
// the first page again after its title changed.
func TestRunContextStopsARunBlockedOnASlowRequest(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocked := make(chan struct{})
	var once sync.Once
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") != "Second" {
			return 0, "", false
		}
		once.Do(func() { close(blocked) })
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		return http.StatusServiceUnavailable, `{"message":"too late"}`, true
	})

	go func() {
		<-blocked
		cancel()
	}()

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.TrackPages = true

	started := time.Now()
	err := RunContext(ctx, config)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 10*time.Second, "the blocked request was not cut short")

	server.SetFail(nil)
	assert.Equal(t, 0, countPagesTitled(t, server, "Second"),
		"the file being published when the run was cancelled did not finish")
	first := findPageTitled(t, server, "First")

	writeFile(t, dir, "a.md", markdownWithTitle("First Renamed"))
	require.NoError(t, RunContext(context.Background(), config))

	assert.Equal(t, first.ID, findPageTitled(t, server, "First Renamed").ID,
		"the manifest saved by the cancelled run found the page it made")
}

// findPageTitled looks a page up with a client of its own, for the reason
// countPagesTitled gives.
func findPageTitled(t *testing.T, server *confluencetest.Server, title string) *confluence.PageInfo {
	t.Helper()

	page, err := confluence.NewAPI(server.URL, "user", "token", false).FindPage("DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, page, "no page titled %q", title)

	return page
}
