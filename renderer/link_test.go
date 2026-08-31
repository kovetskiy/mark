package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
)

func linkRenderers() []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceLinkRenderer(),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextLegacyRenderer(false),
	}
}

// TestLinkToConfluencePageByTitle covers the ac: destination, which is how a
// document links to another Confluence page by title rather than by URL.
func TestLinkToConfluencePageByTitle(t *testing.T) {
	actual := render(t, "[Text](ac:Page)\n", linkRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual,
		`<ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[Text]]></ac:plain-text-link-body></ac:link>`)
}

// TestLinkTitleIsEscapedIntoTheAttribute covers the escaping that keeps a page
// title from breaking the document. The title is interpolated into an XML
// attribute, where an unescaped "&" makes the body malformed -- Confluence
// rejects the whole page, not the one link -- and a quote closes the attribute
// early, letting document text inject attributes of its own.
func TestLinkTitleIsEscapedIntoTheAttribute(t *testing.T) {
	amp := render(t, "[A & B](<ac:Q & A>)\n", linkRenderers())
	assertWellFormed(t, amp)
	assert.Contains(t, amp, `ri:content-title="Q &amp; A"`)

	quoted := render(t, "[t](<ac:Say \"hi\">)\n", linkRenderers())
	assertWellFormed(t, quoted)
	assert.NotContains(t, quoted, `ri:content-title="Say "hi""`,
		"a raw quote would close the attribute and let the rest inject more")
	assert.Contains(t, quoted, `Say &#34;hi&#34;`)
}

// TestLinkBodyEscapesTheCDATATerminator covers the other half of the same
// problem. The link text goes inside CDATA, where "]]>" ends the section early;
// splitting it across two sections is the only legal way to carry it.
func TestLinkBodyEscapesTheCDATATerminator(t *testing.T) {
	// A code span is how the sequence gets into link text at all: a bare "]" in
	// a label ends the label, and a backslash-escaped one stays backslashed
	// (see TestLinkBodyKeepsBackslashEscapes).
	actual := render(t, "[a `]]>` b](ac:Page)\n", linkRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<![CDATA[a ]]><![CDATA[]]]]><![CDATA[> b]]>`)
}

// TestLinkBodyKeepsBackslashEscapes records current behaviour rather than
// intended behaviour. The body is built from the link's source text, so a
// Markdown escape inside the label is never resolved and the backslash reaches
// the page: "[foo \_bar\_](ac:Page)" publishes "foo \_bar\_". Inline markup
// fares better -- emphasis is dropped to plain text, which is all
// ac:plain-text-link-body can hold.
func TestLinkBodyKeepsBackslashEscapes(t *testing.T) {
	escaped := render(t, `[foo \_bar\_](ac:Page)`+"\n", linkRenderers())
	assertWellFormed(t, escaped)
	assert.Contains(t, escaped, `<![CDATA[foo \_bar\_]]>`)

	emphasised := render(t, "[**bold** text](ac:Page)\n", linkRenderers())
	assertWellFormed(t, emphasised)
	assert.Contains(t, emphasised, `<![CDATA[bold text]]>`)
}

// TestLinkWithEmptyPageTitleFallsBackToTheText covers a bare "ac:" destination:
// with no title after the prefix, the link text is the title.
func TestLinkWithEmptyPageTitleFallsBackToTheText(t *testing.T) {
	actual := render(t, "[bare](ac:)\n", linkRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ri:content-title="bare"`)
}

// TestOrdinaryLinkKeepsGoldmarkBehaviour covers the case the renderer does not
// change: an ordinary URL is still an <a>, with the destination and title
// escaped for HTML.
func TestOrdinaryLinkKeepsGoldmarkBehaviour(t *testing.T) {
	actual := render(t, "[ext](https://example.com/a?b=1&c=2 \"T & T\")\n", linkRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<a href="https://example.com/a?b=1&amp;c=2" title="T &amp; T">ext</a>`)
}
