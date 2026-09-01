package mark_test

import (
	"os"
	"path/filepath"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// write puts a file in dir and returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// compileFile compiles a document from disk, which is what the include and
// macro paths need in order to resolve anything.
func compileFile(t *testing.T, path string) (string, error) {
	t.Helper()

	source, err := os.ReadFile(path)
	require.NoError(t, err)

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown(source, lib, path, types.MarkConfig{})

	return out, err
}

// TestMacroThatIncludesAFile: a macro's expansion may name an include, and
// includes ran before macros -- so that directive was left for the AST
// transformers, which splice a sub-document parsed from its own bytes into an
// AST rendered against the document's. Every node they did not rewrite kept
// offsets into the wrong buffer: this fenced code block came out with its
// language read from the middle of the word "Include", a body sliced from
// wherever those offsets landed, and NUL bytes past the end.
func TestMacroThatIncludesAFile(t *testing.T) {
	dir := t.TempDir()

	write(t, dir, "sub.md", "intro text\n\n```go\nfunc main() { fmt.Println(\"XYZZY\") }\n```\n\noutro\n")
	write(t, dir, "emit.tmpl", "<!-- Include: sub.md -->\n")
	doc := write(t, dir, "doc.md", "<!-- Macro: GOGO\n     Template: emit.tmpl\n-->\n\nbefore\n\nGOGO\n\nafter\n")

	out, err := compileFile(t, doc)
	require.NoError(t, err)

	assert.Contains(t, out, `<ac:parameter ac:name="language">go</ac:parameter>`,
		"the code block keeps its own language")
	assert.Contains(t, out, `func main() { fmt.Println("XYZZY") }`,
		"and its own body")

	assert.NotContains(t, out, "\x00", "no bytes read past the end of a buffer")
	assert.NotContains(t, out, "lude:", "and none read from the middle of the directive")
	assert.Equal(t, 1, countOf(out, "after"), "the text after the macro is published once")
}

// TestIncludeThatDefinesAMacro is the other direction: a fragment may define a
// macro, and macros were extracted only once, before that fragment was read.
func TestIncludeThatDefinesAMacro(t *testing.T) {
	dir := t.TempDir()

	write(t, dir, "defs.md", "<!-- Macro: TICKET-(\\d+)\n     Template: #inline\n     inline: \"issue ${1}\" -->\n")
	doc := write(t, dir, "doc.md", "<!-- Include: defs.md -->\n\nSee TICKET-42.\n")

	out, err := compileFile(t, doc)
	require.NoError(t, err)

	assert.Contains(t, out, "issue 42", "a macro an include defined is applied")
}

func countOf(haystack, needle string) int {
	var n int
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			n++
		}
	}

	return n
}
