package header

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHeader(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func newStdlib(t *testing.T) *stdlib.Lib {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	return std
}

func TestNoPathIsNoHeader(t *testing.T) {
	header, err := Load("", newStdlib(t))
	require.NoError(t, err)
	assert.Nil(t, header)

	html, attachments, err := header.Render("doc.md", "Title", "SPACE", types.MarkConfig{})
	require.NoError(t, err)
	assert.Empty(t, html)
	assert.Empty(t, attachments)
}

func TestStorageFormatHeader(t *testing.T) {
	t.Chdir(t.TempDir())

	path := writeHeader(t, "header.html",
		`<p><a href="https://github.com/org/repo/blob/main/{{ .EscapedPath | xmlesc }}">{{ .Path | xmlesc }}</a> in {{ .Space }}: {{ .Title | xmlesc }}</p>`)

	header, err := Load(path, newStdlib(t))
	require.NoError(t, err)

	html, _, err := header.Render(filepath.Join("docs", "my notes.md"), "A & B", "DOCS", types.MarkConfig{})
	require.NoError(t, err)
	assert.Equal(t,
		`<p><a href="https://github.com/org/repo/blob/main/docs/my%20notes.md">docs/my notes.md</a> in DOCS: A &amp; B</p>`,
		html)
}

func TestMarkdownHeader(t *testing.T) {
	path := writeHeader(t, "header.md", "> [!WARNING]\n> Generated from [{{ .Path }}](https://example.com/{{ .EscapedPath }}).\n\n# Not dropped\n")

	header, err := Load(path, newStdlib(t))
	require.NoError(t, err)

	html, _, err := header.Render("doc.md", "Title", "DOCS", types.MarkConfig{
		DropFirstH1: true,
		Features:    []string{"mention"},
	})
	require.NoError(t, err)
	assert.Contains(t, html, `<ac:structured-macro ac:name="note">`)
	assert.Contains(t, html, `<a href="https://example.com/doc.md">doc.md</a>`)
	assert.Contains(t, html, `Not dropped</h1>`)
}

func TestMalformedStorageFormatHeaderFailsAtLoad(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>unclosed`)

	_, err := Load(path, newStdlib(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
}

// TestTemplateCannotReadTheEnvironment: the environment holds the Confluence
// password, and the header is published on every page.
func TestTemplateCannotReadTheEnvironment(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>{{ env "MARK_PASSWORD" }}</p>`)

	_, err := Load(path, newStdlib(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `function "env" not defined`)
}

func TestHeaderDoesNotJoinTheDocumentTemplates(t *testing.T) {
	std := newStdlib(t)
	path := writeHeader(t, "header.html", `<p>Header</p>`)

	_, err := Load(path, std)
	require.NoError(t, err)
	assert.Nil(t, std.Templates.Lookup("page-header"))
}
