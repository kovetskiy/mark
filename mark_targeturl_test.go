package mark

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTargetURLTakesOneFile: a page id names one page, so a pattern matching
// several files is refused before anything is published over it.
func TestTargetURLTakesOneFile(t *testing.T) {
	server, _ := docsSpace(t)
	target := server.AddPage("DOCS", "Target", "page", "")
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "# A\n\nFirst.\n")
	writeFile(t, dir, "b.md", "# B\n\nSecond.\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.TrackPages = false
	config.PageID = target.ID

	err := Run(config)
	require.ErrorContains(t, err, "matched 2 files")
	assert.Equal(t, 0, server.CountRequests(http.MethodPut, "/content/"+target.ID), "nothing is published")

	config.Files = filepath.Join(dir, "a.md")
	require.NoError(t, Run(config))
	assert.Contains(t, server.Page(target.ID).Body, "First.")
}
