package mark

import (
	"path/filepath"
	"testing"

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
	server, api := docsSpace(t)
	dir := t.TempDir()

	// Named so the good one is processed first and the run then fails.
	writeFile(t, dir, "a-good.md", markdownWithTitle("Published"))
	writeFile(t, dir, "b-bad.md", "no metadata here at all\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.Error(t, Run(config), "the run must still report the failure")

	published, err := api.FindPage("DOCS", "Published", "page")
	require.NoError(t, err)
	require.NotNil(t, published, "the good file published")

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup("DOCS", filepath.Join(dir, "a-good.md"))
	require.NoError(t, err)
	require.True(t, ok, "the page that published must still be in the manifest")
	assert.Equal(t, published.ID, entry.PageID)
}
