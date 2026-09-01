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

// TestBlockQuoteMarkerMustOpenTheLine covers the trap this syntax used to set.
// The classifier looked for the word anywhere in the first line, so a quotation
// that merely mentioned one of four very common words became an admonition
// macro -- and the author's only recourse was to reword the sentence. The
// marker now has to open the line, which is where every documented form of it
// is written anyway.
func TestBlockQuoteMarkerMustOpenTheLine(t *testing.T) {
	prose := []struct {
		name  string
		input string
	}{
		{"word inside another word", "> Nothing here was noteworthy.\n"},
		{"info inside information", "> Information about pricing is below.\n"},
		{"note mid-sentence", "> Please note that the API changed.\n"},
		{"tip inside multiple", "> There are multiple ways to do this.\n"},
		{"warn inside a sentence", "> We should warn them first.\n"},
	}

	for _, tt := range prose {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, legacyBlockQuoteRenderers())
			assertWellFormed(t, actual)

			assert.Contains(t, actual, "<blockquote>",
				"ordinary prose stays a quotation")
			assert.NotContains(t, actual, "ac:structured-macro")
		})
	}
}

// TestBlockQuoteMarkerFormsStillClassify is the other half: everything the
// syntax has always accepted has to keep working, or the fix above would be a
// removal rather than a repair.
func TestBlockQuoteMarkerFormsStillClassify(t *testing.T) {
	forms := []struct {
		name  string
		input string
		want  string
	}{
		{"marker with a colon and text", "> Info: the build is nightly.\n", `ac:name="info"`},
		{"marker alone", "> Warn\n", `ac:name="warning"`},
		{"plural with a colon", "> **NOTES:**\n>\n> One.\n", `ac:name="note"`},
		{"emphasised marker", "> **Tip:** use the cache.\n", `ac:name="tip"`},
		{"shouting", "> NOTE: shouting.\n", `ac:name="note"`},
		{"warning spelled out", "> Warning: this deletes data.\n", `ac:name="warning"`},
	}

	for _, tt := range forms {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, legacyBlockQuoteRenderers())
			assertWellFormed(t, actual)

			assert.Contains(t, actual, tt.want)
		})
	}
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
