package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

// The <p> wrapper was dropped whenever a paragraph merely *began* with a raw
// fragment, so the prose after the tag was emitted bare at block level -- while
// the same tag written mid-sentence kept its wrapper.
func TestParagraphOpeningWithConfluenceTagKeepsItsWrapper(t *testing.T) {
	out := compileParagraphDoc(t, `<ac:emoticon ac:name="smile"/> and then trailing prose.`+"\n")

	assert.Equal(t, "<p><ac:emoticon ac:name=\"smile\"/> and then trailing prose.</p>\n", out)
}

// A paragraph that is nothing but the fragment is the fragment, and a <p>
// around a Confluence block element is not something Confluence accepts.
func TestParagraphOfOnlyAConfluenceTagStaysUnwrapped(t *testing.T) {
	out := compileParagraphDoc(t, `<ac:emoticon ac:name="smile"/>`+"\n")

	assert.Equal(t, "<ac:emoticon ac:name=\"smile\"/>\n", out)
}

// Hand-written inline HTML that opens and closes within the paragraph is the
// author's own element, and gets no wrapper -- this is what the old blanket
// rule was protecting and it has to keep working.
func TestParagraphOfOneHandWrittenElementStaysUnwrapped(t *testing.T) {
	out := compileParagraphDoc(t, "<b>bold</b>\n")

	assert.Equal(t, "<b>bold</b>\n", out)
}

// An author building a Confluence element across several blocks writes its
// opening tag on a line of its own; a <p> there would interleave with the
// element.
func TestParagraphOpeningAConfluenceElementStaysUnwrapped(t *testing.T) {
	out := compileParagraphDoc(t, "<ac:structured-macro ac:name=\"info\"> leading text\n")

	assert.Equal(t, "<ac:structured-macro ac:name=\"info\"> leading text\n", out)
}

// compileParagraphDoc compiles a document through the default path with the
// stdlib templates loaded. Defined here rather than shared so this fix stands
// on its own.
func compileParagraphDoc(t *testing.T, src string, features ...string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{Features: features})
	require.NoError(t, err)

	return out
}

// TestParagraphWithTwoElementsOfTheSameName: the wrapper was dropped whenever
// the paragraph's first and last fragments were tags of the same name, which is
// true of prose bracketed by two separate elements. The text between them ended
// up at bare body level.
func TestParagraphWithTwoElementsOfTheSameName(t *testing.T) {
	out := compileParagraphDoc(t, "<em>Hello</em> world <em>again</em>\n")

	assert.Contains(t, out, "<p><em>Hello</em> world <em>again</em></p>",
		"two elements of the same name are not one element around the prose")
}

// TestParagraphWithACompleteConfluenceElement: an ac: element that opens and
// closes inside the paragraph is complete, so whatever follows it is prose and
// belongs in the wrapper. The opening tag was judged on its own, and an
// <ac:link> is not self-closing, so the paragraph was unwrapped and the trailing
// prose left at body level.
func TestParagraphWithACompleteConfluenceElement(t *testing.T) {
	out := compileParagraphDoc(t, "<ac:link>foo</ac:link> and more prose.\n")

	assert.Contains(t, out, "<p><ac:link>foo</ac:link> and more prose.</p>")
}

// TestParagraphThatOnlyOpensAConfluenceElement is the case both of the above
// have to leave alone: a half of an element spread over several blocks, where a
// <p> would interleave with the element being built.
func TestParagraphThatOnlyOpensAConfluenceElement(t *testing.T) {
	out := compileParagraphDoc(t, "<ac:layout-cell>\n\nInside.\n\n</ac:layout-cell>\n")

	assert.NotContains(t, out, "<p><ac:layout-cell>")
	assert.Contains(t, out, "<p>Inside.</p>")
}
