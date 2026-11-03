package transformer_test

import (
	"bytes"
	"testing"

	"github.com/kovetskiy/mark/v16/internal/goldmarktest"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

func TestDetailsTransformer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "details with summary and content",
			input:    "<details><summary>Summary Title</summary><p>Body Content</p></details>",
			expected: "<ac:structured-macro ac:name=\"expand\"><ac:parameter ac:name=\"title\">Summary Title</ac:parameter><ac:rich-text-body><p>Body Content</p></ac:rich-text-body></ac:structured-macro>",
		},
		{
			// The title is interpolated text, so it goes through XMLEscape: a
			// character XML 1.0 cannot carry is replaced, not sent to Confluence.
			name:     "summary title escaped as XML",
			input:    "<details><summary>Fish &amp; chips \x01</summary><p>Body Content</p></details>",
			expected: "<ac:parameter ac:name=\"title\">Fish &amp; chips \uFFFD</ac:parameter>",
		},
		{
			name:     "details without summary",
			input:    "<details><p>Body Content</p></details>",
			expected: "<ac:structured-macro ac:name=\"expand\"><ac:rich-text-body><p>Body Content</p></ac:rich-text-body></ac:structured-macro>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gm := goldmarktest.New(
				goldmarktest.WithRendererOptions(
					html.WithExtensions(transformer.NewHTMLRenderer()),
					html.WithUnsafe(),
					html.WithExtensions(
						crenderer.NewConfluenceTextRenderer(false),
					),
				),
				goldmarktest.WithParserOptions(
					parser.WithASTTransformers(
						util.Prioritized[parser.ASTTransformer](transformer.NewDetailsTransformer(), 110),
					),
				),
			)

			var buf bytes.Buffer
			err := gm.Convert([]byte(tt.input), &buf)
			assert.NoError(t, err)
			assert.Contains(t, buf.String(), tt.expected)
		})
	}
}
