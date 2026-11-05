package renderer_test

import (
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v17/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/renderer/html"
)

func blockQuoteRenderers() []html.Extension {
	return []html.Extension{
		crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextRenderer(false),
	}
}

// TestBlockQuoteClassification covers the colon syntax, "> Info: text".
//
// The words are not interchangeable with the macros they produce: "warn"
// selects Confluence's warning macro, and "note" selects info, as GitHub's
// [!NOTE] does, so a mapping slip shows up as the wrong colour and icon on the
// page rather than as an error anywhere.
func TestBlockQuoteClassification(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"info", "> Info: the build is nightly.\n", `ac:name="info"`},
		{"note is info", "> Note: this is worth knowing.\n", `ac:name="info"`},
		{"warning from warn", "> Warn: this deletes data.\n", `ac:name="warning"`},
		{"tip", "> Tip: use the cache.\n", `ac:name="tip"`},
		{"case insensitive", "> NOTE: shouting.\n", `ac:name="info"`},
		{"markup around the word", "> **Note:** emphasised.\n", `ac:name="info"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, blockQuoteRenderers())
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
	actual := render(t, "> An ordinary quotation.\n", blockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<blockquote>")
	assert.Contains(t, actual, "</blockquote>")
	assert.NotContains(t, actual, "ac:structured-macro")
}

// TestBlockQuoteProseStaysAQuote covers quotations that start with, or
// mention, one of the words without the colon that makes it a marker. These
// are ordinary sentences, and an author should not have to reword one to keep
// it a quote.
func TestBlockQuoteProseStaysAQuote(t *testing.T) {
	prose := []struct {
		name  string
		input string
	}{
		{"word inside another word", "> Nothing here was noteworthy.\n"},
		{"info inside information", "> Information about pricing is below.\n"},
		{"note mid-sentence", "> Please note that the API changed.\n"},
		{"tip inside multiple", "> There are multiple ways to do this.\n"},
		{"warn inside a sentence", "> We should warn them first.\n"},
		{"note opening a sentence", "> Note that this is a quote.\n"},
		{"tips opening a sentence", "> Tips are welcome.\n"},
		{"warning opening a sentence", "> Warning signs were everywhere.\n"},
		{"word alone", "> Warn\n"},
		{"emphasised word alone", "> **Note**\n>\n> Text.\n"},
		{"space before the colon", "> Note : spaced.\n"},
		{"no space after the colon", "> Note:spaced.\n"},
		{"word in code", "> `Note:` in code.\n"},
		{"word in a list", "> * Note: in a list.\n"},
		{"word on the second line", "> Some text\n> Note: later.\n"},
	}

	for _, tt := range prose {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, blockQuoteRenderers())
			assertWellFormed(t, actual)

			assert.Contains(t, actual, "<blockquote>",
				"ordinary prose stays a quotation")
			assert.NotContains(t, actual, "ac:structured-macro")
		})
	}
}

// TestBlockQuoteMarkerForms covers every way of writing the marker: the word
// and its colon, in any case, emphasised with the colon inside or outside, or
// alone in a comment.
func TestBlockQuoteMarkerForms(t *testing.T) {
	forms := []struct {
		name  string
		input string
		want  string
	}{
		{"marker with a colon and text", "> Info: the build is nightly.\n", `ac:name="info"`},
		{"marker alone with its colon", "> Warn:\n>\n> Text.\n", `ac:name="warning"`},
		{"plural with a colon", "> **NOTES:**\n>\n> One.\n", `ac:name="info"`},
		{"emphasised marker", "> **Tip:** use the cache.\n", `ac:name="tip"`},
		{"colon outside the emphasis", "> **Tip**: use the cache.\n", `ac:name="tip"`},
		{"italic marker", "> *Warning:* hot.\n", `ac:name="warning"`},
		{"shouting", "> NOTE: shouting.\n", `ac:name="info"`},
		{"mixed case", "> wArNiNgS: mixed.\n", `ac:name="warning"`},
		{"warning spelled out", "> Warning: this deletes data.\n", `ac:name="warning"`},
		{"heading", "> ### Tip: a heading\n", `ac:name="tip"`},
		{"comment", "> <!-- Tip -->\n> Text.\n", `ac:name="tip"`},
		{"comment with a colon", "> <!-- Tip: -->\n> Text.\n", `ac:name="tip"`},
	}

	for _, tt := range forms {
		t.Run(tt.name, func(t *testing.T) {
			actual := render(t, tt.input, blockQuoteRenderers())
			assertWellFormed(t, actual)

			assert.Contains(t, actual, tt.want)
		})
	}
}

// TestBlockQuoteCommentMustHoldOnlyTheWord covers the comment form: a comment
// that says more than the word is a comment, not a marker.
func TestBlockQuoteCommentMustHoldOnlyTheWord(t *testing.T) {
	actual := render(t, "> <!-- Note that this is a draft -->\n> Text.\n", blockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<blockquote>")
	assert.NotContains(t, actual, "ac:structured-macro")
}

// TestBlockQuoteClassifierZeroValue pins that the exported classifier cannot
// be built in a broken state: its zero value classifies as the constructor's
// does.
func TestBlockQuoteClassifierZeroValue(t *testing.T) {
	var zero crenderer.BlockQuoteClassifier

	assert.NotPanics(t, func() { zero.ClassifyingBlockQuote("Note: x") })
	assert.Equal(t, crenderer.AdmonitionInfo, zero.ClassifyingBlockQuote("Note: x"))
	assert.Equal(t, crenderer.AdmonitionNone, zero.ClassifyingBlockQuote("Note that x"))
	assert.Equal(t, crenderer.LegacyBlockQuoteClassifier(), zero)
}

// TestBlockQuoteTypeComesFromTheFirstParagraphOnly pins the scope of the
// classification. A quote whose second paragraph says "warning" must not be
// reclassified by it -- the type belongs to the opening line.
func TestBlockQuoteTypeComesFromTheFirstParagraphOnly(t *testing.T) {
	actual := render(t, "> Info: the first line.\n>\n> A warning in the second.\n", blockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:name="info"`)
	assert.NotContains(t, actual, `ac:name="warning"`)
}

// TestBlockQuoteNestedStaysAQuote covers a quote inside a classified quote.
// Only the outermost one becomes a macro; nesting a macro inside its own
// rich-text body is not something the editor accepts, and the level map is what
// keeps that from happening.
func TestBlockQuoteNestedStaysAQuote(t *testing.T) {
	actual := render(t, "> Note: the outer one.\n>\n> > The inner one.\n", blockQuoteRenderers())
	assertWellFormed(t, actual)

	assert.Equal(t, 1, strings.Count(actual, "<ac:structured-macro"),
		"only the outer quote becomes a macro")
	assert.Equal(t, 1, strings.Count(actual, "<blockquote>"))
	assert.Equal(t, 1, strings.Count(actual, "</blockquote>"))
}

// TestParseBlockQuoteTypeString covers the names the enum prints, which are
// interpolated straight into ac:name.
func TestParseBlockQuoteTypeString(t *testing.T) {
	assert.Equal(t, "info", crenderer.AdmonitionInfo.String())
	assert.Equal(t, "note", crenderer.AdmonitionNote.String())
	assert.Equal(t, "warning", crenderer.AdmonitionWarning.String())
	assert.Equal(t, "tip", crenderer.AdmonitionTip.String())
	assert.Equal(t, "none", crenderer.AdmonitionNone.String())
}
