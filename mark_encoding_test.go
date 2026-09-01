package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestByteOrderMarkDoesNotHideMetadata: a BOM in front of the first header
// comment made every parser here see a file with no metadata at all, and the
// run failed with "doesn't contain metadata" -- pointing at the headers, which
// are present and correct. Windows editors write a BOM routinely, and "UTF-8
// with BOM" is a one-click setting in VS Code.
func TestByteOrderMarkDoesNotHideMetadata(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := filepath.Join(dir, "doc.md")
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: With BOM -->\n\nBody text.\n"
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o600))

	require.NoError(t, Run(publishConfig(server.URL, path)))

	assert.Equal(t, 1, countPagesTitled(t, server, "With BOM"))
	assert.NotContains(t, bodyOfPageTitled(t, server, "With BOM"), "\ufeff",
		"the mark itself must not reach the page")
}

// TestByteOrderMarkDoesNotHideFrontMatter covers the other metadata form. The
// BOM sits in front of the opening "---", so it stops being a front-matter
// fence and becomes a horizontal rule followed by the YAML as body text.
func TestByteOrderMarkDoesNotHideFrontMatter(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := filepath.Join(dir, "doc.md")
	body := "---\nspace: DOCS\ntitle: BOM Front Matter\nparents: [ Parent ]\n---\n\nBody text.\n"
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o600))

	config := publishConfig(server.URL, path)
	config.Features = append(config.Features, "frontmatter")
	require.NoError(t, Run(config))

	assert.Equal(t, 1, countPagesTitled(t, server, "BOM Front Matter"))
	assert.NotContains(t, bodyOfPageTitled(t, server, "BOM Front Matter"), "space: DOCS",
		"the front matter must be read as metadata, not published as text")
}
