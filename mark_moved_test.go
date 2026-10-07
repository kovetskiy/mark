package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMovedDocumentKeepsItsPage: a document moved to another directory with its
// title unchanged is found by that title, and the page follows it. The path it
// moved from must not then read as an orphan - its page is the one just
// published, and --on-orphan delete would trash it in the same run.
func TestMovedDocumentKeepsItsPage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o755))
	writeFile(t, dir, "a/doc.md", markdownWithTitle("Moved"))
	writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.OnOrphan = "delete"
	require.NoError(t, Run(config))

	before, err := api.FindPage("DOCS", "Moved", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	require.NoError(t, os.Rename(filepath.Join(dir, "a", "doc.md"), filepath.Join(dir, "b", "doc.md")))
	require.NoError(t, Run(config))

	page := server.Page(before.ID)
	require.NotNil(t, page)
	assert.False(t, page.Trashed, "the moved document's page must not be trashed")

	// Deleting the moved document afterwards still removes its page.
	require.NoError(t, os.Remove(filepath.Join(dir, "b", "doc.md")))
	require.NoError(t, Run(config))
	assert.True(t, server.Page(before.ID).Trashed)
}

// TestNoOverwriteKeepsAnEditOnTheRunADocumentMoves: the run on which a
// document moves is the one run where its new path has no entry, so the
// version --no-overwrite compares against is in the entry of the path it moved
// from. Asking the new path alone found no baseline and overwrote an edit made
// in Confluence -- and the run after, had the move not carried the version
// across, would have done the same.
func TestNoOverwriteKeepsAnEditOnTheRunADocumentMoves(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Moved"))

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.NoOverwrite = true
	require.NoError(t, Run(config))

	published, err := api.FindPage("DOCS", "Moved", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	server.EditPage(published.ID, "<p>Written by a person.</p>")
	edited := server.Page(published.ID)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(dir, "doc.md"), filepath.Join(dir, "sub", "doc.md")))

	require.NoError(t, Run(config))
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(published.ID).Body,
		"the run that moves the document must not overwrite the edit")
	assert.Equal(t, edited.Version, server.Page(published.ID).Version)

	require.NoError(t, Run(config))
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(published.ID).Body,
		"nor may the run after it")
}
