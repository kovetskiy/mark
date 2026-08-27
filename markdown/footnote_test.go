package mark_test

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
)

func compileFootnotes(t *testing.T, markdown string, features ...string) string {
	t.Helper()

	lib, err := stdlib.New(nil)
	assert.NoError(t, err)

	actual, _, err := mark.CompileMarkdown([]byte(markdown), lib, "testdata/test.md", types.MarkConfig{
		Features: features,
	})
	assert.NoError(t, err)

	return actual
}

// assertWellFormed parses the body the way Confluence does. Confluence answers
// a body that is not well-formed with BadRequestException and rejects the whole
// page, so a stray tag here would not show up as one broken footnote but as a
// document that never uploads.
func assertWellFormed(t *testing.T, body string) {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(`<root xmlns:ac="ac" xmlns:ri="ri">` + body + `</root>`))
	decoder.Strict = true
	decoder.Entity = xml.HTMLEntity

	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if !assert.NoError(t, err, "storage format must be well-formed XML") {
			return
		}
	}
}

func TestFootnotesFeature(t *testing.T) {
	markdown := "A claim[^src].\n\n[^src]: Where it came from.\n"

	enabled := compileFootnotes(t, markdown, "footnotes")
	assertWellFormed(t, enabled)

	// The marker links down to the note, and the note carries the anchor that
	// link names. Both halves have to be present for the jump to land: an
	// ac:link naming an anchor no macro declares is silently inert.
	assert.Contains(t, enabled, `<ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>`)
	assert.Contains(t, enabled, `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>`)

	// And the same in the other direction, so a reader can get back to the
	// sentence that sent them down.
	assert.Contains(t, enabled, `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro>`)
	assert.Contains(t, enabled, `<ac:link ac:anchor="footnote-ref-1">`)

	// Nothing that depends on an id survives the trip through Confluence, so
	// none of it may be relied on.
	assert.NotContains(t, enabled, `id="fn:1"`)
	assert.NotContains(t, enabled, `href="#fn:1"`)

	disabled := compileFootnotes(t, markdown, "mermaid", "mention")
	assert.Contains(t, disabled, `id="fn:1"`, "without the feature goldmark's own HTML is left alone")
	assert.NotContains(t, disabled, `ac:name="anchor"`)
}

// TestFootnotesRepeatedCitation covers a note cited more than once: the ways
// back have to be told apart, which means one anchor per citation rather than
// one per note.
func TestFootnotesRepeatedCitation(t *testing.T) {
	actual := compileFootnotes(t, "First[^a]. Second[^a].\n\n[^a]: Once.\n", "footnotes")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="">footnote-ref-1</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="">footnote-ref-1-1</ac:parameter>`)

	// Numbered arrows, so the two ways back are distinguishable.
	assert.Contains(t, actual, `<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>`)
	assert.Contains(t, actual, `<ac:link ac:anchor="footnote-ref-1-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link>`)

	// One note, cited twice, is still one entry in the list.
	assert.Equal(t, 1, strings.Count(actual, "<li>"))

	// A note cited once gets a bare arrow, with nothing to disambiguate.
	single := compileFootnotes(t, "Only[^a].\n\n[^a]: Once.\n", "footnotes")
	assert.Contains(t, single, `<ac:link-body>&#x21a9;&#xfe0e;</ac:link-body>`)
}

// TestFootnotesNumberingFollowsCitationOrder pins the property the <ol> relies
// on. The list takes no numbers of its own -- it lets Confluence number the
// items -- which is only correct because goldmark orders the notes by the index
// it gave each marker, not by where the definitions were written.
func TestFootnotesNumberingFollowsCitationOrder(t *testing.T) {
	actual := compileFootnotes(t, "Second[^b] then first[^a].\n\n[^a]: A.\n[^b]: B.\n", "footnotes")
	assertWellFormed(t, actual)

	assert.Less(t,
		strings.Index(actual, `<ac:parameter ac:name="">footnote-1</ac:parameter>`),
		strings.Index(actual, `<ac:parameter ac:name="">footnote-2</ac:parameter>`),
		"notes must be listed in the order their markers are numbered")

	assert.Less(t,
		strings.Index(actual, "B."),
		strings.Index(actual, "A."),
		"the note cited first is note 1, whichever was defined first")
}

// TestFootnotesUncitedDefinitionDropped records that a definition nothing
// points at is left out rather than given a number, which is what keeps the
// <ol> counting in step with the markers.
func TestFootnotesUncitedDefinitionDropped(t *testing.T) {
	actual := compileFootnotes(t, "Cited[^a].\n\n[^a]: Used.\n[^b]: Never used.\n", "footnotes")
	assertWellFormed(t, actual)

	assert.NotContains(t, actual, "Never used.")
	assert.Equal(t, 1, strings.Count(actual, "<li>"))
}

// TestFootnotesEscaping covers a note whose text would break the body if it
// reached the storage format as written. Nothing out of the document is
// interpolated into the footnote markup itself -- the anchor names and the
// marker are generated from an integer -- so what this really pins is that the
// note's own text still goes through the ordinary renderers.
func TestFootnotesEscaping(t *testing.T) {
	actual := compileFootnotes(t, "Claim[^a].\n\n[^a]: A & B, \"quoted\" and 3 < 4.\n", "footnotes")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "A &amp; B")
	assert.Contains(t, actual, "3 &lt; 4")
}
