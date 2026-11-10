package page_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPageOutsideItsParentsIsAPlacementWarning: a page found by title
// somewhere other than under its declared parents is moved, and the document
// is told so with a kind of its own rather than only in the log.
func TestAPageOutsideItsParentsIsAPlacementWarning(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Anchor", "page", home.ID)
	elsewhere := server.AddPage("DOCS", "Elsewhere", "page", home.ID)
	server.AddPage("DOCS", "Doc", "page", elsewhere.ID)

	warnings := report.NewWarnings("docs/doc.md")
	_, found, _, err := page.PreviewPage(t.Context(), api, &metadata.Meta{
		Space:   "DOCS",
		Type:    "page",
		Title:   "Doc",
		Parents: []string{"Anchor"},
		Folders: []string{"Manuals"},
	}, page.AncestryOptions{FolderCache: page.NewFolderCache(), Warnings: warnings})
	require.NoError(t, err)
	require.NotNil(t, found)

	list := warnings.List()
	require.Len(t, list, 1)
	assert.Equal(t, report.WarningPlacement, list[0].Kind)
	assert.Equal(t, "docs/doc.md", list[0].File)
	assert.Contains(t, list[0].Message, `page "Doc" exists outside MARK_PARENTS "Anchor"`)
}
