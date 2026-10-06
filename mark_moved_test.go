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
