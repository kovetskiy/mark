package mark

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifestIsSavedWhenAFileFailsHard: the mapping records pages that have
// already been published. Returning on the first bad file skipped the save
// entirely, so one malformed document threw away the identity of every page
// that had published before it -- and the next run resolved all of them by
// title alone, with no rename detection and no --no-overwrite baseline.
//
// The sibling case, --continue-on-error, was fixed long ago and its comment
// explains exactly why. This is the path that comment did not cover.
func TestManifestIsSavedWhenAFileFailsHard(t *testing.T) {
	ctx := t.Context()
	server, api := docsSpace(t)
	dir := t.TempDir()

	// Named so the good one is processed first and the run then fails.
	writeFile(t, dir, "a-good.md", markdownWithTitle("Published"))
	writeFile(t, dir, "b-bad.md", "no metadata here at all\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.Error(t, Run(config), "the run must still report the failure")

	published, err := api.FindPage(context.Background(), "DOCS", "Published", "page")
	require.NoError(t, err)
	require.NotNil(t, published, "the good file published")

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup(ctx, "DOCS", filepath.Join(dir, "a-good.md"))
	require.NoError(t, err)
	require.True(t, ok, "the page that published must still be in the manifest")
	assert.Equal(t, published.ID, entry.PageID)
}

// TestManifestSaveOnTheWayOutOutlastsASignal: the save on the way out of a
// run that failed went out under the run's own context whenever that context
// was still live, so a signal arriving during the save cut it short with the
// manifest half-written. It is made under a detached context whatever the
// state of the run's.
func TestManifestSaveOnTheWayOutOutlastsASignal(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-good.md", markdownWithTitle("Published"))
	writeFile(t, dir, "b-bad.md", "no metadata here at all\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The save's first write stops the run, as a signal would, and is then
	// refused if the client gives up on it; a save that does not give up is
	// let through.
	var once sync.Once
	signalled := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet || !strings.Contains(r.URL.Path, "/propert") {
			return 0, "", false
		}

		aborted := false
		once.Do(func() {
			signalled = true
			cancel()
			select {
			case <-r.Context().Done():
				aborted = true
			case <-time.After(500 * time.Millisecond):
			}
		})
		if aborted {
			return http.StatusServiceUnavailable, `{"message":"the client gave up"}`, true
		}

		return 0, "", false
	})

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.Error(t, RunContext(ctx, config), "the run must still report the failure")
	require.True(t, signalled, "the save on the way out wrote a property")

	server.SetFail(nil)

	published, err := api.FindPage(context.Background(), "DOCS", "Published", "page")
	require.NoError(t, err)
	require.NotNil(t, published, "the good file published")

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup(context.Background(), "DOCS", filepath.Join(dir, "a-good.md"))
	require.NoError(t, err)
	require.True(t, ok, "the save went on to the end despite the signal")
	assert.Equal(t, published.ID, entry.PageID)
}
