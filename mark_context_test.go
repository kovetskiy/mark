package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunContextStopsBetweenFiles: Run is an exported entry point, so a library
// caller can be publishing hundreds of documents with no way to ask it to stop.
// Cancellation is checked between files rather than inside one, since part way
// through a page is the one place stopping would leave a page half written.
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

// TestRunContextStopsPartWayThroughAFile: cancellation reaches the requests a
// file makes, so a run stopped while a file is being published does not finish
// that file -- the page it was about to create is never created. What the run
// did before it stopped is still accounted for on the way out: the report is
// written, and the page manifest is saved, which it could not be under the
// cancelled context itself. The second run proves the save: renaming the first
// document finds the page the first run made rather than creating another.
func TestRunContextStopsPartWayThroughAFile(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancelled from inside the second file's first lookup, as a signal
	// arriving while that request is in flight would.
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") == "Second" {
			cancel()
		}
		return 0, "", false
	})

	var out bytes.Buffer
	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.TrackPages = true
	config.OutputFormat = report.FormatJSON
	config.Output = &out

	err := RunContext(ctx, config)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, countPagesTitled(t, server, "First"))
	assert.Equal(t, 0, countPagesTitled(t, server, "Second"),
		"the file being published when the run was cancelled did not finish")

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed), "the report is still written")
	statuses := map[string]string{}
	for _, page := range parsed.Pages {
		statuses[filepath.Base(page.File)] = page.Status
	}
	assert.Equal(t, report.StatusPublished, statuses["a.md"])
	assert.Equal(t, report.StatusFailed, statuses["b.md"])

	server.SetFail(nil)
	first := findPageTitled(t, server, "First")

	writeFile(t, dir, "a.md", markdownWithTitle("First Renamed"))
	config.Output = io.Discard
	require.NoError(t, RunContext(context.Background(), config))

	renamed := findPageTitled(t, server, "First Renamed")
	assert.Equal(t, first.ID, renamed.ID,
		"the manifest saved by the cancelled run found the page it made")
	assert.Equal(t, 0, countPagesTitled(t, server, "First"))
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
