package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

// A void element left open makes the page not well-formed XML, and Confluence
// rejects the whole page for it. `<br>` in a table cell is the standard way to
// write a multi-line cell, so an ordinary Markdown table took the page down.
func TestVoidElementsAreClosedInTableCells(t *testing.T) {
	out := compileRawHTMLDoc(t, "| a |\n| --- |\n| 1. one<br>2. two |\n")

	assert.Contains(t, out, "<td>1. one<br />2. two</td>")
	assert.NotContains(t, out, "<br>")
}

// The same holds for a void element written as its own block.
func TestVoidElementsAreClosedAtBlockLevel(t *testing.T) {
	out := compileRawHTMLDoc(t, "before\n\n<hr>\n\nafter\n")

	assert.Contains(t, out, "<hr />")
	assert.NotContains(t, out, "<hr>")
}

// A void element nothing else claims is closed too. <img> has its own
// transformer and becomes an ac:image, but the rest of the void set -- <input>,
// <hr>, <br>, <col> and the others -- reach the page as raw HTML and would
// otherwise reach it unclosed.
func TestVoidElementsWithNoTransformerAreClosed(t *testing.T) {
	out := compileRawHTMLDoc(t, `<input type="checkbox" checked>`+"\n")

	assert.Contains(t, out, `<input type="checkbox" checked />`)
	assert.NotContains(t, out, `checked>`)
}

// XML forbids `--` inside a comment body, so a note an author left to
// themselves rejected the page. The comment is invisible either way, so the
// hyphens are collapsed rather than the note being thrown away -- dropping the
// node outright would also take the `<!-- Info -->` marker that classifies a
// legacy blockquote, which the blockquote renderer reads at render time.
func TestCommentWithDoubleHyphenIsRepaired(t *testing.T) {
	out := compileRawHTMLDoc(t, "<!-- TODO -- revisit this later -->\n\ntext\n")

	assert.Contains(t, out, "<!-- TODO - revisit this later -->")
	assert.NotContains(t, out, "TODO --")
}

// The same comment written mid-paragraph arrives as an inline fragment rather
// than a block, and needs the same repair.
func TestInlineCommentWithDoubleHyphenIsRepaired(t *testing.T) {
	out := compileRawHTMLDoc(t, "prose <!-- x -- y --> more prose\n")

	assert.Contains(t, out, "<!-- x - y -->")
}

// The marker that turns a blockquote into an info macro is an HTML comment the
// renderer reads out of the AST, so repairing comments must leave it in place.
func TestLegacyBlockQuoteMarkerSurvives(t *testing.T) {
	out := compileRawHTMLDoc(t, "> <!-- Info -->\n> Test\n")

	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
	assert.Contains(t, out, "<!-- Info -->")
}

// Confluence's own markup is well-formed already and must be passed through
// exactly as written; `<ac:emoticon .../>` is not an unclosed void element.
func TestConfluenceMarkupIsNotRewritten(t *testing.T) {
	out := compileRawHTMLDoc(t, `<ac:emoticon ac:name="smile"/>`+"\n")

	assert.Contains(t, out, `<ac:emoticon ac:name="smile"/>`)
}

// compileRawHTMLDoc compiles a document through the default path with the
// stdlib templates loaded. Defined here rather than shared so this fix stands
// on its own.
func compileRawHTMLDoc(t *testing.T, src string, features ...string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{Features: features})
	require.NoError(t, err)

	return out
}
