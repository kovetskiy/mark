package renderer_test

import (
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
)

func footnoteRender(t *testing.T, source string) string {
	t.Helper()

	return renderExtended(t, source,
		[]goldmark.Extender{extension.Footnote},
		[]renderer.NodeRenderer{
			crenderer.NewConfluenceFootnoteRenderer(newStdlib(t)),
			crenderer.NewConfluenceParagraphRenderer(),
			crenderer.NewConfluenceTextRenderer(false),
		})
}

// TestFootnoteBothEndsCarryAnAnchor covers the property the whole feature rests
// on. Confluence discards the ids goldmark's own footnote output links with, so
// the jump is rebuilt out of the anchor macro -- and an ac:link naming an
// anchor that no macro declares is silently inert, so both ends have to be
// present for either direction to work.
func TestFootnoteBothEndsCarryAnAnchor(t *testing.T) {
	actual := footnoteRender(t, "A claim[^src].\n\n[^src]: Where it came from.\n")
	assertWellFormed(t, actual)

	// Down: an anchor at the marker, and a link to the note.
	assert.Contains(t, actual, `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro>`)
	assert.Contains(t, actual, `<ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>`)

	// Back: an anchor at the note, and a link to the marker.
	assert.Contains(t, actual, `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>`)
	assert.Contains(t, actual, `<ac:link ac:anchor="footnote-ref-1">`)

	// Nothing that depends on an id, because none of it survives the upload.
	assert.NotContains(t, actual, `id="fn:1"`)
	assert.NotContains(t, actual, `href="#fn:1"`)
}

// TestFootnoteListIsAnOrdinaryOrderedList covers the shape of the notes at the
// foot of the page: a rule and an <ol> whose items carry no numbers of their
// own, because Confluence numbering the list is what keeps it in step with the
// markers.
func TestFootnoteListIsAnOrdinaryOrderedList(t *testing.T) {
	actual := footnoteRender(t, "One[^a] and two[^b].\n\n[^a]: First.\n[^b]: Second.\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<hr />\n<ol>")
	assert.Equal(t, 2, strings.Count(actual, "<li>"))
	assert.NotContains(t, actual, `class="footnotes"`,
		"the wrapping div is dropped: Confluence keeps neither its class nor its role")
}

// TestFootnoteRepeatedCitationNumbersTheWayBack covers a note cited twice. Each
// citation needs its own anchor, or the second arrow leads back to the first
// sentence.
func TestFootnoteRepeatedCitationNumbersTheWayBack(t *testing.T) {
	actual := footnoteRender(t, "First[^a]. Second[^a].\n\n[^a]: Once.\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="">footnote-ref-1</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="">footnote-ref-1-1</ac:parameter>`)
	assert.Contains(t, actual, `<sup>1</sup></ac:link-body>`)
	assert.Contains(t, actual, `<sup>2</sup></ac:link-body>`)
	assert.Equal(t, 1, strings.Count(actual, "<li>"), "one note, cited twice, is still one entry")
}

// TestFootnoteSingleCitationHasABareArrow is the other half of that rule: with
// only one way back there is nothing to disambiguate, so the arrow carries no
// number.
func TestFootnoteSingleCitationHasABareArrow(t *testing.T) {
	actual := footnoteRender(t, "Only[^a].\n\n[^a]: Once.\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:link-body>&#x21a9;&#xfe0e;</ac:link-body>`)
}
