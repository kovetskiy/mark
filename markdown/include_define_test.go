package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
}

// TestIncludedDefinesStayWithTheirFragment covers a fragment that defines a
// template of its own. Fragments were parsed straight into the template set
// shared by every page in a run, each only once, so a {{ define }} replaced
// the template for everything after it: a page including its own fragment
// published the one some other page's fragment had defined last.
func TestIncludedDefinesStayWithTheirFragment(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a/frag.md": `{{ define "hdr" }}header-a{{ end }}{{ template "hdr" }}` + "\n",
		"b/frag.md": `{{ define "hdr" }}header-b{{ end }}{{ template "hdr" }}` + "\n",
	})

	for name, compile := range compilers {
		t.Run(name, func(t *testing.T) {
			lib, err := stdlib.New(nil)
			require.NoError(t, err)

			doc := []byte("<!-- Include: frag.md -->\n")
			for _, page := range []struct{ path, want string }{
				{"a/one.md", "header-a"},
				{"b/one.md", "header-b"},
				{"a/two.md", "header-a"},
			} {
				out, _, err := compile(doc, lib, filepath.Join(dir, page.path), types.MarkConfig{})
				require.NoError(t, err, page.path)
				assert.Contains(t, out, page.want, page.path)
			}
		})
	}
}

// TestIncludedDefinesServeTheRestOfThePage covers what a definition is still
// for: a shared file of definitions and macros that use them, and fragments
// included after it on the same page. Keeping definitions from leaking into
// other pages must not keep them from the page that included them.
func TestIncludedDefinesServeTheRestOfThePage(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"macros.md": `{{ define "mybox" }}<b>{{ .text }}</b>{{ end }}` + "\n" +
			"<!-- Macro: BOX-(\\w+)\n     Template: mybox\n     text: ${1} -->\n",
		"later.md": `[{{ template "mybox" . }}]` + "\n",
	})

	doc := []byte("<!-- Include: macros.md -->\n\n# Doc\n\nsee BOX-hello\n\n<!-- Include: later.md\ntext: sibling -->\n")

	for name, compile := range compilers {
		t.Run(name, func(t *testing.T) {
			lib, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile(doc, lib, filepath.Join(dir, "doc.md"), types.MarkConfig{})
			require.NoError(t, err)
			assert.Contains(t, out, "<p>see <b>hello</b></p>")
			assert.Contains(t, out, "[<b>sibling</b>]")
		})
	}
}

// TestIncludedDefineDoesNotReplaceStdlib covers a fragment defining a name the
// stdlib already uses. It may do so for itself, but the pages compiled after
// it -- and the renderers, which draw on the same set -- must still get the
// stdlib's template.
func TestIncludedDefineDoesNotReplaceStdlib(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a/frag.md": `{{ define "ac:box" }}HIJACKED{{ end }}{{ template "ac:box" }}` + "\n",
	})

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte("<!-- Include: frag.md -->\n"), lib, filepath.Join(dir, "a/one.md"), types.MarkConfig{})
	require.NoError(t, err)
	assert.Contains(t, out, "HIJACKED")

	out, _, err = CompileMarkdown([]byte("<!-- Include: ac:box\nName: info\nBody: hello -->\n"), lib, filepath.Join(dir, "b/two.md"), types.MarkConfig{})
	require.NoError(t, err)
	assert.NotContains(t, out, "HIJACKED")
	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
}

// TestIncludedDefineReachesNestedInclude covers the one place a fragment's
// definitions are still meant to be seen: the fragments it includes itself.
func TestIncludedDefineReachesNestedInclude(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"outer.md": `{{ define "hdr" }}from-outer{{ end }}<!-- Include: inner.md -->` + "\n",
		"inner.md": `[{{ template "hdr" }}]` + "\n",
	})

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte("<!-- Include: outer.md -->\n"), lib, filepath.Join(dir, "page.md"), types.MarkConfig{})
	require.NoError(t, err)
	assert.Contains(t, out, "[from-outer]")
}

// TestInlineMacroDefineDoesNotReplaceStdlib is the same for the body of an
// inline macro, which was parsed into the shared set just as a fragment was.
func TestInlineMacroDefineDoesNotReplaceStdlib(t *testing.T) {
	const macroPage = "<!-- Macro: BOX\n" +
		"     Template: #inline\n" +
		"     inline: '{{ define \"ac:box\" }}HIJACKED{{ end }}{{ template \"ac:box\" }}' -->\n\n" +
		"BOX\n"

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(macroPage), lib, filepath.Join(t.TempDir(), "a.md"), types.MarkConfig{})
	require.NoError(t, err)
	assert.Contains(t, out, "HIJACKED")

	out, _, err = CompileMarkdown([]byte("<!-- Include: ac:box\nName: info\nBody: hello -->\n"), lib, filepath.Join(t.TempDir(), "b.md"), types.MarkConfig{})
	require.NoError(t, err)
	assert.NotContains(t, out, "HIJACKED")
	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
}
