package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWithBOM writes a document behind a UTF-8 byte-order mark, the way a
// Windows editor set to "UTF-8 with BOM" does.
func writeWithBOM(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, content...), 0o600))
	return path
}

// storedBody returns what is actually stored on the page carrying a title.
func storedBody(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	found, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", title, "page")
	require.NoError(t, err)
	if found == nil {
		return ""
	}
	return server.Page(found.ID).Body
}

// TestByteOrderMarkDoesNotHideMetadata: a BOM in front of the first header
// comment made every parser here see a file with no metadata at all, and the
// run failed with "doesn't contain metadata" -- pointing away from the headers,
// which are present and correct. "UTF-8 with BOM" is a one-click setting in VS
// Code, and PowerShell redirection produces one without being asked.
func TestByteOrderMarkDoesNotHideMetadata(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := writeWithBOM(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: With BOM -->\n\nBody text.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: path, Features: []string{"mention"}, Output: io.Discard,
	}))

	assert.Equal(t, 1, countPagesTitled(t, server, "With BOM"))
	assert.NotContains(t, storedBody(t, server, "With BOM"), "\ufeff",
		"the mark itself must not reach the page")
}

// TestByteOrderMarkDoesNotHideFrontMatter covers the other metadata form. The
// BOM sits in front of the opening "---", so it stops being a front-matter
// fence and becomes a horizontal rule with the YAML published beneath it as
// body text.
func TestByteOrderMarkDoesNotHideFrontMatter(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := writeWithBOM(t, dir, "doc.md",
		"---\nspace: DOCS\ntitle: BOM Front Matter\nparents: [ Parent ]\n---\n\nBody text.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: path, Features: []string{"mention", "frontmatter"}, Output: io.Discard,
	}))

	assert.Equal(t, 1, countPagesTitled(t, server, "BOM Front Matter"))
	assert.NotContains(t, storedBody(t, server, "BOM Front Matter"), "space: DOCS",
		"the front matter must be read as metadata, not published as text")
}
