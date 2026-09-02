package mark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileBeside compiles a document written next to the given files, so that an
// include has something real to find or miss.
func compileBeside(t *testing.T, source string, files map[string]string) (string, error) {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		[]byte(source), std, filepath.Join(dir, "doc.md"), types.MarkConfig{},
	)

	return out, err
}

// TestIncludeInACodeSpanIsNotRun: the pass that expands includes before
// goldmark sees the document skips code regions, spans included. The AST
// transformer that picks up what it left had no equivalent check and matched a
// paragraph at a time, so a sentence documenting the syntax pulled the file in
// and dropped the code span it was quoted in.
func TestIncludeInACodeSpanIsNotRun(t *testing.T) {
	out, err := compileBeside(t,
		"To include, write `<!-- Include: snippet.md -->` in your doc.\n",
		map[string]string{"snippet.md": "SECRET FRAGMENT BODY"},
	)
	require.NoError(t, err)

	assert.NotContains(t, out, "SECRET FRAGMENT BODY", "the file was not pulled in")
	assert.Contains(t, out, "<code>", "and the span is still a span")
	assert.Contains(t, out, "Include: snippet.md", "showing what the author wrote")
	assert.Contains(t, out, "in your doc.")
}

// TestIncludeInACodeSpanNamingAMissingFileStillCompiles: worse than the wrong
// output, an example naming a file that does not exist failed the whole compile
// -- so writing about mark's own syntax broke the page doing it.
func TestIncludeInACodeSpanNamingAMissingFileStillCompiles(t *testing.T) {
	out, err := compileBeside(t,
		"Write `<!-- Include: nope.md -->` to include a file.\n", nil,
	)
	require.NoError(t, err)

	assert.Contains(t, out, "Include: nope.md")
}

// TestMacroInACodeSpanIsNotDefined: a macro directive quoted as an example was
// registered for real, and the paragraph around it deleted outright.
func TestMacroInACodeSpanIsNotDefined(t *testing.T) {
	out, err := compileBeside(t,
		"Write `<!-- Macro: :hi:\nTemplate: #inline\ninline: \"BOOM\" -->` to define it.\n\n"+
			"Then :hi: appears.\n", nil,
	)
	require.NoError(t, err)

	// "BOOM" appears once, inside the example the author quoted -- and nowhere
	// else, because :hi: below was never given a meaning.
	assert.Equal(t, 1, strings.Count(out, "BOOM"))
	assert.Contains(t, out, "Then :hi: appears.", "the macro was not applied")
	assert.Contains(t, out, "to define it.", "and the paragraph around it survives")
}

// TestIncludeOutsideACodeSpanStillRuns is the other half. The directive is only
// inert where it is quoted.
func TestIncludeOutsideACodeSpanStillRuns(t *testing.T) {
	out, err := compileBeside(t,
		"Before.\n\n<!-- Include: snippet.md -->\n\nAfter.\n",
		map[string]string{"snippet.md": "PULLED IN"},
	)
	require.NoError(t, err)

	assert.Contains(t, out, "PULLED IN")
	assert.Contains(t, out, "Before.")
	assert.Contains(t, out, "After.")
}

// TestIncludeInAFencedBlockIsStillNotRun: fenced blocks were never affected --
// they hold no child nodes for the transformer to concatenate -- and stay that
// way.
func TestIncludeInAFencedBlockIsStillNotRun(t *testing.T) {
	out, err := compileBeside(t,
		"```\n<!-- Include: snippet.md -->\n```\n",
		map[string]string{"snippet.md": "SECRET FRAGMENT BODY"},
	)
	require.NoError(t, err)

	assert.NotContains(t, out, "SECRET FRAGMENT BODY")
}
