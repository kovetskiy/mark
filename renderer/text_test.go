package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
)

// TestTextSoftBreak covers --strip-linebreaks, which exists because Confluence
// turns a newline in the body into a visible line break.
//
// A document written one-sentence-per-line therefore publishes as a ragged
// column unless the soft breaks are flattened to spaces. Both text renderers
// have to agree on this: the legacy one and the GitHub Alerts one are separate
// implementations of the same rule.
func TestTextSoftBreak(t *testing.T) {
	const source = "first line\nsecond line\n"

	tests := []struct {
		name      string
		renderers []renderer.NodeRenderer
		want      string
	}{
		{
			name: "legacy keeps the newline",
			renderers: []renderer.NodeRenderer{
				crenderer.NewConfluenceTextLegacyRenderer(false),
				crenderer.NewConfluenceParagraphRenderer(),
			},
			want: "<p>first line\nsecond line</p>",
		},
		{
			name: "legacy strips the newline",
			renderers: []renderer.NodeRenderer{
				crenderer.NewConfluenceTextLegacyRenderer(true),
				crenderer.NewConfluenceParagraphRenderer(),
			},
			want: "<p>first line second line</p>",
		},
		{
			name: "alerts renderer keeps the newline",
			renderers: []renderer.NodeRenderer{
				crenderer.NewConfluenceTextRenderer(false),
				crenderer.NewConfluenceParagraphRenderer(),
			},
			want: "<p>first line\nsecond line</p>",
		},
		{
			name: "alerts renderer strips the newline",
			renderers: []renderer.NodeRenderer{
				crenderer.NewConfluenceTextRenderer(true),
				crenderer.NewConfluenceParagraphRenderer(),
			},
			want: "<p>first line second line</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, render(t, source, tt.renderers), tt.want)
		})
	}
}

// TestTextHardBreakSurvivesStripping covers the line break the author asked for
// with two trailing spaces. Stripping soft breaks must not take it: it is the
// only way to get a line break inside a paragraph on purpose.
func TestTextHardBreakSurvivesStripping(t *testing.T) {
	const source = "first line  \nsecond line\n"

	for _, strip := range []bool{false, true} {
		actual := render(t, source, []renderer.NodeRenderer{
			crenderer.NewConfluenceTextRenderer(strip),
			crenderer.NewConfluenceParagraphRenderer(),
		})
		assertWellFormed(t, actual)

		assert.Contains(t, actual, "<br />", "strip-linebreaks=%v must keep a hard break", strip)
	}
}

// TestTextEscapesMarkupCharacters covers the escaping every other renderer
// relies on: prose reaches the page through this one, and an unescaped "&" or
// "<" makes the whole body malformed, which Confluence rejects outright.
func TestTextEscapesMarkupCharacters(t *testing.T) {
	actual := render(t, "A & B, 3 < 4, \"quoted\"\n", []renderer.NodeRenderer{
		crenderer.NewConfluenceTextRenderer(false),
		crenderer.NewConfluenceParagraphRenderer(),
	})
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "A &amp; B")
	assert.Contains(t, actual, "3 &lt; 4")
}
