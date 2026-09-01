package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileMath compiles a document with the math feature on, which is the only
// way any of this is reachable.
func compileMath(t *testing.T, src string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{
		Features:   []string{"math"},
		MathFormat: "svg",
	})
	require.NoError(t, err)

	return out
}

// TestMathBlockSpansABlankLine is what the block form exists for. An aligned
// environment is written with blank lines in it, and an inline parser is handed
// one block at a time -- so the formula was two blocks to goldmark and could not
// be seen whole from inside either.
//
// Reading past the end of the block instead, which is what the inline parser
// did before it was bounded, swallowed the paragraphs in between and published
// them twice.
func TestMathBlockSpansABlankLine(t *testing.T) {
	out := compileMath(t, `$$
\begin{aligned}
a &= b \\

c &= d
\end{aligned}
$$

Prose after.
`)

	assert.Contains(t, out, `c &amp;= d`, "the whole environment is one formula")
	assert.Equal(t, 1, strings.Count(out, "<ac:image"), "and it is one image")

	assert.Contains(t, out, "<p>Prose after.</p>",
		"the paragraph after it is published once, as itself")
	assert.NotContains(t, out, "<p>\\begin{aligned}",
		"and the formula's source is not published beside its picture")
}

// TestMathBlockInABlockquoteDropsThePrefix: the block's lines come from
// goldmark's own segments, which have already had the "> " taken off. The
// inline parser reads contiguous source instead, so the prefix went to LaTeX.
func TestMathBlockInABlockquoteDropsThePrefix(t *testing.T) {
	out := compileMath(t, "> $$\n> E = mc^2\n> $$\n")

	assert.Contains(t, out, `ac:alt="E = mc^2"`)
	assert.NotContains(t, out, `ac:alt="&gt;`)
	assert.Contains(t, out, "<blockquote>", "and it stays inside the quotation")
}

// TestMathBlockInAListItemDropsTheIndent is the same property for the other
// construct that prefixes its lines.
func TestMathBlockInAListItemDropsTheIndent(t *testing.T) {
	out := compileMath(t, "- item\n\n  $$\n  E = mc^2\n  $$\n")

	assert.Contains(t, out, `ac:alt="E = mc^2"`)
	assert.NotContains(t, out, `ac:alt="  `)
}

// TestSingleLineFormulaIsUnchanged: a formula that opens and closes on one line
// is the inline parser's, whether it stands alone or sits in a sentence. The
// block parser must not claim it -- it has always worked, and taking it would
// move the picture out of the paragraph it belongs to.
func TestSingleLineFormulaIsUnchanged(t *testing.T) {
	alone := compileMath(t, "$$E = mc^2$$\n")
	assert.Contains(t, alone, "<p><ac:image", "still a paragraph of its own")

	inSentence := compileMath(t, "The identity $$E = mc^2$$ is famous.\n")
	assert.Contains(t, inSentence, "<p>The identity <ac:image")
	assert.Contains(t, inSentence, "is famous.</p>")
}

// TestMathFenceInsideACodeBlockStaysCode: the block parser sits below
// goldmark's fenced code block, so a document explaining the syntax publishes
// the example rather than rendering it.
func TestMathFenceInsideACodeBlockStaysCode(t *testing.T) {
	out := compileMath(t, "```markdown\n$$\nE = mc^2\n$$\n```\n")

	assert.Contains(t, out, `ac:name="code"`)
	assert.Contains(t, out, "$$", "the fence is published as the example it is")
	assert.NotContains(t, out, "<ac:image")
}

// TestUnclosedMathFenceDoesNotSwallowTheDocument: an opening fence with no
// closer takes the rest of the document, which is what a fenced code block does
// too. What matters is that it is one formula rather than a parse that runs
// away.
func TestUnclosedMathFenceDoesNotSwallowTheDocument(t *testing.T) {
	out := compileMath(t, "$$\nE = mc^2\n\nstill inside\n")

	assert.Equal(t, 1, strings.Count(out, "<ac:image"))
}

// TestBracketFenceIsABlock: "\[" on a line of its own opens a display formula
// the same way "$$" does. Read as an inline the formula carried the blockquote
// marker into LaTeX -- and "> " is valid in math mode, so MathJax typeset it
// happily and published a quietly wrong picture rather than failing.
func TestBracketFenceIsABlock(t *testing.T) {
	quoted := compileMath(t, "> \\[\n> E = mc^2\n> \\]\n")

	assert.Contains(t, quoted, `ac:alt="E = mc^2"`)
	assert.NotContains(t, quoted, "&gt; E = mc")
	assert.Contains(t, quoted, "<blockquote>")

	// The equation no longer carries the newlines the markers sat on either.
	plain := compileMath(t, "\\[\nE = mc^2\n\\]\n")
	assert.Contains(t, plain, `ac:alt="E = mc^2"`)
}

// TestBracketsInProseStayLiteral: "\[" is CommonMark's escape for a literal
// bracket, so a sentence using it is prose. Parsing it as a formula published a
// picture of the word and uploaded an attachment for it.
func TestBracketsInProseStayLiteral(t *testing.T) {
	out := compileMath(t, "Use \\[brackets\\] literally here.\n")

	assert.NotContains(t, out, "<ac:image")
	assert.Contains(t, out, "[brackets]")
}

// TestBracketFormulaAloneOnALineStillRenders is the line between the two above:
// display math written on its own line, opening and closing together, is what
// the README documents and what testdata/math.md carries.
func TestBracketFormulaAloneOnALineStillRenders(t *testing.T) {
	out := compileMath(t, "Text.\n\n\\[\\begin{pmatrix} a & b \\end{pmatrix}\\]\n\nMore.\n")

	assert.Contains(t, out, "<ac:image")
	assert.Contains(t, out, `\begin{pmatrix}`)
}

// TestLongerDollarFenceIsMatchedByItsOwnLength: a run of three was read as a
// "$$" fence with a stray "$" after it, which the inline parser then published
// beside the image.
func TestLongerDollarFenceIsMatchedByItsOwnLength(t *testing.T) {
	out := compileMath(t, "$$$\nE = mc^2\n$$$\n")

	assert.Contains(t, out, `ac:alt="E = mc^2"`)
	assert.Equal(t, 1, strings.Count(out, "<ac:image"))
	assert.NotContains(t, out, "<p>$</p>")
	assert.NotContains(t, out, "</ac:image>$")
}
