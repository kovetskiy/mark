package renderer_test

import (
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
)

func legacyBlockQuoteRenderers() []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceBlockQuoteRenderer(),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextLegacyRenderer(false),
	}
}

// TestBlockQuoteClassification covers the legacy "> Info: ..." syntax, which is
// what mark supported before GitHub Alerts and still supports.
//
// The four names are not interchangeable with the macros they produce: "warn"
// selects Confluence's warning macro, and "note" selects note, so a mapping
// slip shows up as the wrong colour and icon on the page rather than as an
// error anywhere.
func TestBlockQuoteClassification(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"info", "> Info: the build is nightly.\n", `ac:name="info"`},
		{"note", "> Note: this is worth knowing.\n", `ac:name="note"`},
		{"warning from warn", "> Warn: this deletes data.\n", `ac:name="warning"`},
		{"tip", "> Tip: use the cache.\n", `ac:name="tip"`},
		{"case insensitive", "> NOTE: shouting.\n", `ac:name="note"`},
		{"markup around the word", "> **Note:** emphasised.\n", `ac:name="note"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, legacyBlockQuoteRenderers())
			assertWellFormed(t, actual)

			assert.Contains(t, actual, tt.want)
			assert.Contains(t, actual, `<ac:parameter ac:name="icon">true</ac:parameter>`)
			assert.Contains(t, actual, "</ac:rich-text-body></ac:structured-macro>")
			assert.NotContains(t, actual, "<blockquote>")
		})
	}
}

// TestBlockQuoteUnclassifiedStaysAQuote covers the quote that names none of the
// four types: it has to stay a blockquote rather than become a macro of some
// default kind.
func TestBlockQuoteUnclassifiedStaysAQuote(t *testing.T) {
	actual := render(t, "> An ordinary quotation.\n", legacyBlockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<blockquote>")
	assert.Contains(t, actual, "</blockquote>")
	assert.NotContains(t, actual, "ac:structured-macro")
}

// TestBlockQuoteClassifierMatchesAnywhereInTheFirstLine records a trap in the
// legacy syntax rather than an intention: the classifier looks for the word
// anywhere in the first line, unanchored and case-insensitively, so a quote that
// merely mentions the word becomes a macro. Authors who want a plain quotation
// of a sentence containing "note" have to reword it or use a different
// construct.
func TestBlockQuoteClassifierMatchesAnywhereInTheFirstLine(t *testing.T) {
	actual := render(t, "> Nothing here was noteworthy.\n", legacyBlockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:name="note"`,
		"the word inside another word still classifies the quote")
}

// TestBlockQuoteTypeComesFromTheFirstParagraphOnly pins the scope of the
// classification. A quote whose second paragraph says "warning" must not be
// reclassified by it -- the type belongs to the opening line.
func TestBlockQuoteTypeComesFromTheFirstParagraphOnly(t *testing.T) {
	actual := render(t, "> Info: the first line.\n>\n> A warning in the second.\n", legacyBlockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:name="info"`)
	assert.NotContains(t, actual, `ac:name="warning"`)
}

// TestBlockQuoteNestedStaysAQuote covers a quote inside a classified quote.
// Only the outermost one becomes a macro; nesting a macro inside its own
// rich-text body is not something the editor accepts, and the level map is what
// keeps that from happening.
func TestBlockQuoteNestedStaysAQuote(t *testing.T) {
	actual := render(t, "> Note: the outer one.\n>\n> > The inner one.\n", legacyBlockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Equal(t, 1, strings.Count(actual, "<ac:structured-macro"),
		"only the outer quote becomes a macro")
	assert.Equal(t, 1, strings.Count(actual, "<blockquote>"))
	assert.Equal(t, 1, strings.Count(actual, "</blockquote>"))
}

// TestParseBlockQuoteTypeString covers the names the enum prints, which are
// interpolated straight into ac:name.
func TestParseBlockQuoteTypeString(t *testing.T) {
	assert.Equal(t, "info", crenderer.Info.String())
	assert.Equal(t, "note", crenderer.Note.String())
	assert.Equal(t, "warning", crenderer.Warn.String())
	assert.Equal(t, "tip", crenderer.Tip.String())
	assert.Equal(t, "none", crenderer.None.String())
}
