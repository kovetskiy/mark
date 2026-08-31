package renderer_test

import (
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func ghAlertRenderers() []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(),
		crenderer.NewConfluenceTextRenderer(false),
		crenderer.NewConfluenceParagraphRenderer(),
	}
}

func ghAlertParserOptions() []parser.Option {
	return []parser.Option{
		parser.WithASTTransformers(util.Prioritized(ctransformer.NewGHAlertsTransformer(), 100)),
	}
}

// TestGHAlertMacroMapping pins which Confluence macro each GitHub alert becomes.
//
// The two vocabularies do not line up: GitHub has five alert types and
// Confluence four macros, and the names that do exist on both sides do not mean
// the same thing. GitHub's "warning" is the amber one and its "caution" is the
// red one, so warning maps to Confluence's note (amber) and caution to warning
// (red). Getting this backwards produces a page that renders perfectly and
// tells the reader the opposite of what the author meant about how dangerous
// something is, which no test of well-formedness would catch.
func TestGHAlertMacroMapping(t *testing.T) {
	tests := []struct {
		alert string
		want  string
	}{
		{"NOTE", "info"},
		{"TIP", "tip"},
		{"IMPORTANT", "info"},
		{"WARNING", "note"},
		{"CAUTION", "warning"},
	}

	for _, tt := range tests {
		t.Run(tt.alert, func(t *testing.T) {
			actual := render(t, "> [!"+tt.alert+"]\n> The body.\n", ghAlertRenderers(), ghAlertParserOptions()...)
			assertWellFormed(t, actual)

			assert.Contains(t, actual, `<ac:structured-macro ac:name="`+tt.want+`">`)
			assert.Contains(t, actual, "The body.")
			assert.NotContains(t, actual, "<blockquote>")
			// The marker itself is not content; it chose the macro.
			assert.NotContains(t, actual, "[!"+tt.alert+"]")
		})
	}
}

// TestGHAlertRendererFallsBackToLegacySyntax covers the reason this renderer
// replaces the legacy one rather than sitting beside it: with GitHub Alerts on,
// it is the only thing registered for blockquotes, so it still has to handle
// the "> Note:" syntax that predates them.
func TestGHAlertRendererFallsBackToLegacySyntax(t *testing.T) {
	actual := render(t, "> Warn: the old syntax.\n", ghAlertRenderers(), ghAlertParserOptions()...)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:name="warning"`)
}

// TestGHAlertRendererLeavesPlainQuotesAlone covers the third case the same
// renderer has to serve: a quotation that is neither an alert nor the legacy
// syntax must come out as a blockquote.
func TestGHAlertRendererLeavesPlainQuotesAlone(t *testing.T) {
	actual := render(t, "> Just a quotation.\n", ghAlertRenderers(), ghAlertParserOptions()...)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<blockquote>")
	assert.NotContains(t, actual, "ac:structured-macro")
}

// TestGHAlertNestedQuoteStaysAQuote covers a quotation inside an alert. Only
// the alert becomes a macro; the inner quote stays a blockquote, and the tags
// still balance.
func TestGHAlertNestedQuoteStaysAQuote(t *testing.T) {
	actual := render(t, "> [!NOTE]\n> The body.\n>\n> > Quoted inside.\n", ghAlertRenderers(), ghAlertParserOptions()...)
	assertWellFormed(t, actual)

	assert.Equal(t, 1, strings.Count(actual, "<ac:structured-macro"))
	assert.Equal(t, 1, strings.Count(actual, "</ac:structured-macro>"))
	assert.Equal(t, 1, strings.Count(actual, "<blockquote>"))
	assert.Equal(t, 1, strings.Count(actual, "</blockquote>"))
}

// TestGHAlertsInSuccessionEachGetTheirOwnMacro covers the renderer's one piece
// of state: it remembers the blockquote it opened so that it closes the right
// one. Two alerts in a row are what that has to survive.
func TestGHAlertsInSuccessionEachGetTheirOwnMacro(t *testing.T) {
	actual := render(t, "> [!NOTE]\n> First.\n\ntext\n\n> [!CAUTION]\n> Second.\n", ghAlertRenderers(), ghAlertParserOptions()...)
	assertWellFormed(t, actual)

	assert.Equal(t, 2, strings.Count(actual, "<ac:structured-macro"))
	assert.Equal(t, 2, strings.Count(actual, "</ac:structured-macro>"))
	assert.Contains(t, actual, `ac:name="info"`)
	assert.Contains(t, actual, `ac:name="warning"`)
}
