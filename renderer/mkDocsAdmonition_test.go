package renderer_test

import (
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	mkDocsParser "github.com/stefanfritsch/goldmark-admonitions"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func admonitionRenderers() []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceMkDocsAdmonitionRenderer(),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextRenderer(false),
	}
}

func admonitionParserOptions() []parser.Option {
	return []parser.Option{
		parser.WithBlockParsers(util.Prioritized(mkDocsParser.NewAdmonitionParser(), 100)),
	}
}

// TestAdmonitionMacroMapping covers the four MkDocs classes that have a
// Confluence macro. Unlike GitHub Alerts, the names line up one to one here --
// except that MkDocs "warning" is Confluence's warning, where GitHub's
// "warning" is Confluence's note.
func TestAdmonitionMacroMapping(t *testing.T) {
	tests := []struct {
		class string
		want  string
	}{
		{"info", "info"},
		{"note", "note"},
		{"warning", "warning"},
		{"tip", "tip"},
	}

	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			actual := render(t, "!!! "+tt.class+"\n    The body.\n", admonitionRenderers(), admonitionParserOptions()...)
			assertWellFormed(t, actual)

			assert.Contains(t, actual, `<ac:structured-macro ac:name="`+tt.want+`">`)
			assert.Contains(t, actual, `<ac:parameter ac:name="icon">true</ac:parameter>`)
			assert.Contains(t, actual, "The body.")
		})
	}
}

// TestAdmonitionUnknownClassStaysAQuote covers a class Confluence has no macro
// for -- MkDocs has a dozen of them. It has to degrade to a blockquote rather
// than pick a macro at random or drop the content.
//
// The quote keeps the attributes the admonition parser put on it, and one of
// them is a randomly generated data-admonition id. That makes the published
// body differ between two runs over an unchanged document, so an unknown class
// is worth avoiding for anything published repeatedly.
func TestAdmonitionUnknownClassStaysAQuote(t *testing.T) {
	actual := render(t, "!!! danger\n    Mind the gap.\n", admonitionRenderers(), admonitionParserOptions()...)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<blockquote")
	assert.Contains(t, actual, `class="admonition adm-danger"`)
	assert.Contains(t, actual, "Mind the gap.")
	assert.NotContains(t, actual, "ac:structured-macro")
}

// TestAdmonitionTitle covers the quoted title, which Confluence's macros have
// no parameter for and which is therefore written as a bold first paragraph
// inside the body.
func TestAdmonitionTitle(t *testing.T) {
	actual := render(t, "!!! note \"NOTES:\"\n    The body.\n", admonitionRenderers(), admonitionParserOptions()...)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<p><strong>NOTES:</strong></p>")
}

// TestAdmonitionTitleIsEscaped covers a title carrying markup characters. It is
// written into the body as markup, so an unescaped "&" or "<" would make the
// document malformed and cost the whole page.
func TestAdmonitionTitleIsEscaped(t *testing.T) {
	actual := render(t, "!!! note \"A & B <script>\"\n    The body.\n", admonitionRenderers(), admonitionParserOptions()...)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "A &amp; B &lt;script&gt;")
}

// TestAdmonitionNested covers an admonition inside an admonition, which MkDocs
// documents and which the fixtures in testdata/ use. Both become macros: unlike
// blockquotes, the inner one is not degraded.
func TestAdmonitionNested(t *testing.T) {
	actual := render(t, "!!! note \"Outer\"\n    Text.\n\n    !!! tip\n        Inner.\n", admonitionRenderers(), admonitionParserOptions()...)
	assertWellFormed(t, actual)

	assert.Equal(t, 2, strings.Count(actual, "<ac:structured-macro"))
	assert.Equal(t, 2, strings.Count(actual, "</ac:structured-macro>"))
	assert.Contains(t, actual, `ac:name="tip"`)
}
