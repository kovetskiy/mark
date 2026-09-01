package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileLinkDoc(t *testing.T, src string) string {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	return out
}

// TestLinkToAnAnchorOnAnotherPage: a "#" in an ac: destination names a section,
// not part of the title -- and there was no way to write one, though storage
// format has said it with ac:anchor beside ri:page all along.
func TestLinkToAnAnchorOnAnotherPage(t *testing.T) {
	out := compileLinkDoc(t, "See [the setup](<ac:Other Page#Setup>).\n")

	assert.Contains(t, out, `<ac:link ac:anchor="Setup"><ri:page ri:content-title="Other Page"/>`)
	assert.NotContains(t, out, `content-title="Other Page#Setup"`,
		"the anchor is not part of the title")
}

// TestPageLinkWithoutAnAnchorIsUnchanged is the control.
func TestPageLinkWithoutAnAnchorIsUnchanged(t *testing.T) {
	out := compileLinkDoc(t, "See [the page](<ac:Other Page>).\n")

	assert.Contains(t, out, `<ac:link><ri:page ri:content-title="Other Page"/>`)
	assert.NotContains(t, out, "ac:anchor")
}

// TestPageTitleMayContainAHash: a "#" alone cannot be the separator. "C# Guide"
// is a page title, and reading it as a page called "C" with an anchor called
// " Guide" would break a link that works today -- so the part after the last
// "#" has to look like an anchor, which means no whitespace in it.
func TestPageTitleMayContainAHash(t *testing.T) {
	alone := compileLinkDoc(t, "[x](<ac:C# Guide>)\n")

	assert.Contains(t, alone, `ri:content-title="C# Guide"`)
	assert.NotContains(t, alone, "ac:anchor")
}

// TestAnchorWithWhitespaceIsPartOfTheTitle is the same rule from the other
// side: an author who means an anchor writes one without spaces, because that
// is what every id mark generates looks like.
func TestAnchorWithWhitespaceIsPartOfTheTitle(t *testing.T) {
	out := compileLinkDoc(t, "[x](<ac:Release Notes#Q1 2026>)\n")

	assert.Contains(t, out, `ri:content-title="Release Notes#Q1 2026"`)
	assert.NotContains(t, out, "ac:anchor")
}

// TestManualAnchorBecomesTheMacro: "<a name=...>" is how an author marks a spot
// no heading names. Confluence keeps neither the name nor the id -- the element
// survives stripped of both -- so the anchor was published as an empty <a> and
// nothing could reach it.
func TestManualAnchorBecomesTheMacro(t *testing.T) {
	out := compileLinkDoc(t, `<a name="details"></a>

Text. See [details](#details).
`)

	assert.Contains(t, out,
		`<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">details</ac:parameter></ac:structured-macro>`)
	assert.Contains(t, out, `<ac:link ac:anchor="details">`)
	assert.NotContains(t, out, "<a name=")
	assert.NotContains(t, out, "</a>")
}

// TestManualAnchorWrittenWithAnID covers the other spelling, and the escaping
// the name needs on its way into a macro parameter.
func TestManualAnchorWrittenWithAnID(t *testing.T) {
	out := compileLinkDoc(t, "<a id=\"a&b\"></a>\n\nText.\n")

	assert.Contains(t, out, `<ac:parameter ac:name="">a&amp;b</ac:parameter>`)
}

// TestOrdinaryLinksAreNotAnchors is the control: an <a> with an href is a link,
// and its closing tag is not the macro's to take.
func TestOrdinaryLinksAreNotAnchors(t *testing.T) {
	out := compileLinkDoc(t, `<a href="https://example.com">text</a>`+"\n")

	assert.Contains(t, out, `<a href="https://example.com">text</a>`)
	assert.NotContains(t, out, "ac:name=\"anchor\"")
}
