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
// closer is not a fence.
//
// This used to run to the end of the document on the grounds that a fenced code
// block does the same, which CommonMark does say. The two are not comparable in
// what it costs. A code block still publishes what it swallowed, and publishes
// it legibly; a formula publishes a picture of it, or nothing at all -- prose
// is not valid TeX, so the usual outcome is a failed page and an error naming
// the formula rather than the line the fence was opened on.
func TestUnclosedMathFenceDoesNotSwallowTheDocument(t *testing.T) {
	out := compileMath(t, "$$\nE = mc^2\n\nstill inside\n")

	assert.Equal(t, 0, strings.Count(out, "<ac:image"), "nothing was a formula")
	assert.Contains(t, out, "still inside", "and the rest of the document survives")
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

// TestUnclosedBracketFenceLeavesTheDocumentAlone is the same for the other
// fence. "\[" with no "\]" is the CommonMark escape for a literal bracket.
func TestUnclosedBracketFenceLeavesTheDocumentAlone(t *testing.T) {
	out := compileMath(t, "Intro paragraph.\n\n\\[\na + b\n\n## A heading\n\nBody text that must survive.\n")

	assert.Contains(t, out, "<p>Intro paragraph.</p>")
	assert.Contains(t, out, "A heading")
	assert.Contains(t, out, "Body text that must survive.")
	assert.NotContains(t, out, "<ac:image")
}

// TestClosedMathFenceStillOpensABlock is the other half: a fence that does
// close behaves exactly as it always has, wherever its closer happens to be.
func TestClosedMathFenceStillOpensABlock(t *testing.T) {
	out := compileMath(t, "Intro.\n\n$$\na + b\n$$\n\nProse after.\n")

	assert.Contains(t, out, "<ac:image")
	assert.Contains(t, out, "<p>Prose after.</p>")
	assert.NotContains(t, out, "$$")
}

// TestClosedMathFenceInABlockquoteIsStillFound: the lookahead reads the source,
// where a closing fence inside a quotation still carries its "> ". Missing it
// would refuse a formula that closes perfectly well.
func TestClosedMathFenceInABlockquoteIsStillFound(t *testing.T) {
	out := compileMath(t, "> $$\n> \\begin{aligned}\n> a &= b\n>\n> c &= d\n> \\end{aligned}\n> $$\n")

	assert.Contains(t, out, "<ac:image")
	assert.Contains(t, out, "<blockquote>")
}

// TestMathFenceIsNotClosedByOneInsideACodeBlock: the lookahead for a closing
// fence reads the source, so it read the code samples in it too. A document
// showing what a display formula looks like closed a fence opened further up,
// and the formula then swallowed everything between them -- the opening ```
// included, which left the rest of the document inside a code block nobody had
// opened. Silently, and with a zero exit code.
func TestMathFenceIsNotClosedByOneInsideACodeBlock(t *testing.T) {
	out := compileMath(t, "Intro.\n\n$$\na + b\n\nProse that must survive.\n\n```\n$$\n```\n\nTail.\n")

	assert.NotContains(t, out, "<ac:image", "nothing closed the fence, so nothing was a formula")
	assert.Contains(t, out, "Prose that must survive.")
	assert.Contains(t, out, "Tail.")
	assert.Contains(t, out, "<![CDATA[$$]]>", "and the sample is still a code sample")
	require.NoError(t, CheckWellFormed(out))
}

// TestMathFenceIsNotClosedByOneInAnIndentedCodeBlock covers the other kind of
// code block, which the parser knows about and a scan for "$$" does not.
func TestMathFenceIsNotClosedByOneInAnIndentedCodeBlock(t *testing.T) {
	out := compileMath(t, "Intro.\n\n$$\na + b\n\nProse that must survive.\n\n    $$\n\nTail.\n")

	assert.NotContains(t, out, "<ac:image")
	assert.Contains(t, out, "Prose that must survive.")
	assert.Contains(t, out, "Tail.")
}

// TestMathFenceIsStillClosedByOneAfterACodeBlock is the other half: a sample
// showing the syntax does not stop a real closer further down from working.
func TestMathFenceIsStillClosedByOneAfterACodeBlock(t *testing.T) {
	out := compileMath(t, "```\n$$\n```\n\n$$\nE = mc^2\n$$\n\nTail.\n")

	assert.Contains(t, out, `ac:alt="E = mc^2"`, "the real fence still renders")
	assert.Contains(t, out, "<![CDATA[$$]]>", "and the sample is still a sample")
	assert.Contains(t, out, "<p>Tail.</p>")
}

// TestMathFenceInABlockquoteIsNotClosedFromInsideACodeBlock: the two rules meet
// -- a closer inside a quotation still counts, one inside code still does not.
func TestMathFenceInABlockquoteIsNotClosedFromInsideACodeBlock(t *testing.T) {
	out := compileMath(t, "> $$\n> a + b\n\nProse.\n\n```\n> $$\n```\n")

	assert.NotContains(t, out, "<ac:image")
	assert.Contains(t, out, "Prose.")
}
