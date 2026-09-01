package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileAnchorDoc(t *testing.T, src string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	return out
}

// TestSamePageLinkUsesTheAnchorMacro is the whole point. The HTML idiom --
// id="X" on the heading, href="#X" on the link -- renders, is clickable, and
// does nothing, because Confluence keeps no id on a heading and generates its
// own from the element's text. Both ends now use the storage format's own way
// of saying it, which is what the footnote renderers have always emitted.
func TestSamePageLinkUsesTheAnchorMacro(t *testing.T) {
	out := compileAnchorDoc(t, "## Setup Guide\n\nSee [the setup](#setup-guide) below.\n")

	assert.Contains(t, out,
		`<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">Setup-Guide</ac:parameter></ac:structured-macro>`,
		"the heading says where it is")
	assert.Contains(t, out,
		`<ac:link ac:anchor="Setup-Guide"><ac:link-body>the setup</ac:link-body></ac:link>`,
		"and the link goes there")

	assert.NotContains(t, out, `href="#`, "no fragment link survives")
}

// TestOnlyLinkedHeadingsCarryAnAnchor: a macro on every heading would be markup
// nobody reads, on every page. Only the ones something points at get one.
func TestOnlyLinkedHeadingsCarryAnAnchor(t *testing.T) {
	out := compileAnchorDoc(t, "## Linked\n\n## Unlinked\n\n[go](#linked)\n")

	assert.Equal(t, 1, strings.Count(out, `ac:name="anchor"`))
	assert.Contains(t, out, `<ac:parameter ac:name="">Linked</ac:parameter>`)
}

// TestAnchorLinkKeepsItsMarkup: the link body is inline content like any other,
// so emphasis and code inside it still render.
func TestAnchorLinkKeepsItsMarkup(t *testing.T) {
	out := compileAnchorDoc(t, "## Setup\n\n[the **bold** setup](#setup)\n")

	assert.Contains(t, out, `<ac:link-body>the <strong>bold</strong> setup</ac:link-body>`)
}

// TestAnchorIsEscaped: the id goes into an XML attribute and into a macro
// parameter, and a heading may contain anything.
func TestAnchorIsEscaped(t *testing.T) {
	out := compileAnchorDoc(t, "## A & B\n\n[x](#a--b)\n")

	assert.NotContains(t, out, `ac:anchor="A & B"`)
	assert.Contains(t, out, "&amp;")
}

// TestLinksToOtherThingsAreUnchanged is the control: only a "#" destination is
// an anchor on this page.
func TestLinksToOtherThingsAreUnchanged(t *testing.T) {
	out := compileAnchorDoc(t, "[out](https://example.com) and [page](ac:Other Page)\n")

	assert.Contains(t, out, `<a href="https://example.com">out</a>`)
	assert.NotContains(t, out, "ac:anchor")
}
