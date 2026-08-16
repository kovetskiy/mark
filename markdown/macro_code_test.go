package mark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jiraMacro = "<!-- Macro: MYJIRA-\\d+\n" +
	"     Template: ac:jira:ticket\n" +
	"     Ticket: ${0} -->\n\n"

func compile(t *testing.T, path string, markdown string) string {
	t.Helper()

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown(
		[]byte(markdown), lib, path,
		types.MarkConfig{MermaidScale: 1.0, D2Scale: 1.0},
	)
	require.NoError(t, err)

	return out
}

// TestMacroInsideCodeIsLeftAlone is the corruption this fixes. Macros were
// applied to the file's text before it was parsed, so a page documenting a
// macro had its own example expanded -- the one thing the code block existed
// to show.
func TestMacroInsideCodeIsLeftAlone(t *testing.T) {
	out := compile(t, "testdata/x.md", jiraMacro+
		"Prose mentions MYJIRA-123.\n\n"+
		"```\nfenced MYJIRA-456\n```\n\n"+
		"A span `MYJIRA-789` too.\n")

	code := out[strings.Index(out, "<![CDATA["):strings.Index(out, "]]>")]
	assert.Contains(t, code, "MYJIRA-456", "the fenced sample must survive")
	assert.NotContains(t, code, "ac:structured-macro",
		"a macro inside a fenced block must not be expanded")

	assert.Contains(t, out, "<code>MYJIRA-789</code>",
		"a macro inside a code span must not be expanded")

	// ...while a real mention outside code still expands.
	assert.Contains(t, out, `<ac:parameter ac:name="key">MYJIRA-123</ac:parameter>`,
		"prose must still expand")
}

// TestMacroKeepsSurroundingText pins the spacing around an expansion. The
// macro's replacement is parsed on its own, and a document does not begin with
// a space, so the word before the macro ran into it.
func TestMacroKeepsSurroundingText(t *testing.T) {
	out := compile(t, "testdata/x.md", jiraMacro+"Prose mentions MYJIRA-123.\n")

	assert.Contains(t, out, "mentions <ac:structured-macro",
		"the space before the macro must survive")
	assert.Contains(t, out, "</ac:structured-macro>.",
		"the text after the macro must survive")
}

// TestMacroIsNotExpandedTwice guards the pipeline, which runs until the tree
// stops changing. A macro whose output contains the text that matched it --
// which the jira macro does -- would otherwise be expanded into itself.
func TestMacroIsNotExpandedTwice(t *testing.T) {
	out := compile(t, "testdata/x.md", jiraMacro+"Prose mentions MYJIRA-123.\n")

	assert.Equal(t, 1, strings.Count(out, "ac:name=\"jira\""),
		"the macro must be expanded exactly once")
}

// TestIncludeInsideCodeBlockIsLeftAlone is the same corruption for includes: a
// page showing how to write an Include directive had the file pulled in.
func TestIncludeInsideCodeBlockIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "frag.md"), []byte("INCLUDED-CONTENT\n"), 0o600,
	))

	out := compile(t, filepath.Join(dir, "doc.md"),
		"Refer to a fragment like this:\n\n"+
			"```markdown\n<!-- Include: frag.md -->\n```\n")

	assert.NotContains(t, out, "INCLUDED-CONTENT",
		"an Include directive inside a fenced block must not be expanded")
	assert.Contains(t, out, "<![CDATA[<!-- Include: frag.md -->]]>",
		"the directive must survive as written")
}

// TestIncludedContentStillGetsMacros pins the boundary between the two. Text an
// include brings in is text the document asked for, and macros apply to it;
// only a macro's own output is off limits.
func TestIncludedContentStillGetsMacros(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "frag.md"), []byte("Fragment mentions MYJIRA-123.\n"), 0o600,
	))

	out := compile(t, filepath.Join(dir, "doc.md"),
		jiraMacro+"<!-- Include: frag.md -->\n")

	assert.Contains(t, out, `<ac:parameter ac:name="key">MYJIRA-123</ac:parameter>`,
		"a macro must still apply to included text")
}
