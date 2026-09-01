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
// already been published. Returning on the first bad file used to skip the save
// entirely, so one malformed document threw away the identity of every page
// that had published before it -- and the next run resolved all of them by
// title alone, with no rename detection and no --no-overwrite baseline.
//
// The sibling case, --continue-on-error, was fixed long ago; this is the path
// that was missed.
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

// TestPreserveCommentsKeepsARetitle: the retitle is staged on the page object
// and published by the update. --preserve-comments refetches the page in
// between, which brought back the title Confluence still held, and the rename
// was dropped without a word. It compounded: the manifest went on to record the
// new title against a page carrying the old one, so every parent named by that
// title resolved to the wrong place from then on.
func TestPreserveCommentsKeepsARetitle(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Original Title")))
	config.PreserveComments = true
	require.NoError(t, Run(config))

	original, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, original)

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Renamed Title", server.Page(original.ID).Title,
		"the page should have been renamed in place")
	assert.Equal(t, 0, countPagesTitled(t, server, "Original Title"))
}

// TestPreserveCommentsLeavesAnUnchangedTitleAlone is the control: the fix must
// restore the staged title, not stamp the document's title onto every page it
// touches.
func TestPreserveCommentsLeavesAnUnchangedTitleAlone(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Steady")))
	config.PreserveComments = true

	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	assert.Equal(t, 1, countPagesTitled(t, server, "Steady"))
}
