package markdown

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileMarkdownReportsAnIncludeThatChangedFile: the include warning
// reaches the document's warnings through a compile, once, though both the
// expansion before goldmark and the AST transformer may see the directive.
func TestCompileMarkdownReportsAnIncludeThatChangedFile(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"sub/a.md": "A[<!-- Include: b.md -->]",
		"sub/b.md": "beside the fragment",
		"b.md":     "beside the document",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	lib, err := stdlib.New(t.Context(), nil)
	require.NoError(t, err)

	doc := filepath.Join(dir, "doc.md")
	warnings := report.NewWarnings(doc)
	out, _, err := CompileMarkdown(t.Context(), []byte("<!-- Include: sub/a.md -->\n"), lib, doc, Options{Warnings: warnings})
	require.NoError(t, err)
	assert.Contains(t, out, "beside the fragment")

	list := warnings.List()
	require.Len(t, list, 1, "%v", list)
	assert.Equal(t, report.WarningInclude, list[0].Kind)
	assert.Equal(t, doc, list[0].File)
}
