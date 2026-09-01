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

// writeMacroIncludingFragment lays out a document whose include directive is
// produced by a macro. Includes are expanded over the raw bytes before macros
// are, so a directive a macro writes is only ever seen by the AST include
// transformer -- which is the path these tests are about.
func writeMacroIncludingFragment(t *testing.T, fragment string) (dir string, doc string) {
	t.Helper()

	dir = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag.md"), []byte(fragment), 0644))

	return dir, "<!-- Macro: :pull-frag:\n" +
		"Template: #inline\n" +
		"inline: \"<!-- Include: frag.md -->\" -->\n\n" +
		":pull-frag:\n"
}

// compileInDir compiles a document that lives in dir, with no --include-path
// configured, which is the ordinary case.
func compileInDir(t *testing.T, dir string, doc string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(doc), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)

	return out
}

// The AST macro and include transformers were handed the markdown file itself
// as their base directory, so every relative Template: resolved under
// "doc.md/" and could only ever be found when --include-path happened to cover
// it.
func TestASTIncludeResolvesRelativeToDocumentDirectory(t *testing.T) {
	dir, doc := writeMacroIncludingFragment(t, "fragment body\n")

	assert.Contains(t, compileInDir(t, dir, doc), "fragment body")
}
