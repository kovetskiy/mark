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

func TestLayoutTransformer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "layout section single",
			input:    "<!-- ac:layout -->\n<!-- ac:layout-section type:single -->\n<!-- ac:layout-cell -->\nCell content\n<!-- ac:layout-cell end -->\n<!-- ac:layout-section end -->\n<!-- ac:layout end -->",
			expected: "<ac:layout>\n<ac:layout-section ac:type=\"single\">\n<ac:layout-cell>\n<p>Cell content</p>\n</ac:layout-cell>\n</ac:layout-section>\n</ac:layout>",
		},
		{
			name:     "placeholder tags",
			input:    "<!-- ac:placeholder -->\nPlaceholder content\n<!-- ac:placeholder end -->",
			expected: "<ac:placeholder>\n<p>Placeholder content</p>\n</ac:placeholder>",
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
						util.Prioritized[parser.ASTTransformer](transformer.NewLayoutTransformer(), 100),
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
