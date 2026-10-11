package mark

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHeadersDifferingOnlyInCaseKeepThePage: headers that differ from the
// stored titles only in case still name the same page and ancestor, so the
// page is updated where it is rather than moved up under "Home".
func TestHeadersDifferingOnlyInCaseKeepThePage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(config))

	before, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, before)
	parentID := server.Page(before.ID).ParentID

	recased := strings.NewReplacer(
		"<!-- Parent: Parent -->", "<!-- Parent: home -->",
		"<!-- Title: Doc -->", "<!-- Title: doc -->",
	).Replace(markdownWithTitle("Doc"))
	writeFile(t, dir, "doc.md", recased)
	require.NoError(t, Run(config))

	after, err := api.FindPage(context.Background(), "DOCS", "doc", "page")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before.ID, after.ID, "the page the document published is the one it names")
	assert.Equal(t, parentID, server.Page(after.ID).ParentID, "a page under the declared ancestor stays where it is")
}
