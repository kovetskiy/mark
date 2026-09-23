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

// strayOpenerDoc quotes a comment opener in a code span ahead of a real
// directive. The pass that expands includes paired that opener with the
// directive's own "-->", decided the whole stretch was not a directive, and
// resumed scanning after it -- leaving the directive for the AST include
// transformer, which spliced a fenced block in with its language and body read
// out of the wrong buffer, or, on the legacy path, for nothing at all.
const strayOpenerDoc = "Write `<!--` to open a comment in your doc body okay.\n\n" +
	"<!-- Include: inc.md -->\n"

const strayOpenerFragment = "```go\nfunc main() {}\n```\n"

func compileStrayOpener(
	t *testing.T,
	compile func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []any, error),
	doc string,
) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inc.md"), []byte(strayOpenerFragment), 0o600))

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := compile([]byte(doc), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)

	return out
}

func TestIncludeAfterStrayCommentOpener(t *testing.T) {
	paths := map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []any, error){
		"default": func(b []byte, s *stdlib.Lib, p string, c types.MarkConfig) (string, []any, error) {
			out, _, err := CompileMarkdown(b, s, p, c)
			return out, nil, err
		},
		"legacy": func(b []byte, s *stdlib.Lib, p string, c types.MarkConfig) (string, []any, error) {
			out, _, err := CompileMarkdownLegacy(b, s, p, c)
			return out, nil, err
		},
	}

	for name, compile := range paths {
		t.Run(name, func(t *testing.T) {
			out := compileStrayOpener(t, compile, strayOpenerDoc)

			assert.Contains(t, out, `<ac:parameter ac:name="language">go</ac:parameter>`)
			assert.Contains(t, out, "func main() {}")
			assert.NotContains(t, out, "Include: inc.md", "the directive was expanded, not left in the page")
			assert.Contains(t, out, "<code>&lt;!--</code>", "and the quoted opener is still shown")
		})
	}
}

// The macro pass has its own scanner; a stray opener in front of a macro
// directive must not hide it either.
func TestMacroAfterStrayCommentOpener(t *testing.T) {
	doc := "Write `<!--` to open a comment.\n\n" +
		"<!-- Macro: :hi:\nTemplate: #inline\ninline: \"HELLO FROM MACRO\" -->\n\n" +
		":hi:\n"

	for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []any, error){
		"default": func(b []byte, s *stdlib.Lib, p string, c types.MarkConfig) (string, []any, error) {
			out, _, err := CompileMarkdown(b, s, p, c)
			return out, nil, err
		},
		"legacy": func(b []byte, s *stdlib.Lib, p string, c types.MarkConfig) (string, []any, error) {
			out, _, err := CompileMarkdownLegacy(b, s, p, c)
			return out, nil, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := compileStrayOpener(t, compile, doc)

			assert.Contains(t, out, "HELLO FROM MACRO")
			assert.NotContains(t, out, ":hi:")
			assert.NotContains(t, out, "Macro:")
		})
	}
}
