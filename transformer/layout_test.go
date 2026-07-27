package transformer_test

import (
	"bytes"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
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
			gm := goldmark.New(
				goldmark.WithRendererOptions(
					html.WithUnsafe(),
					renderer.WithNodeRenderers(
						util.Prioritized(crenderer.NewConfluenceTextRenderer(false), 200),
					),
				),
				goldmark.WithParserOptions(
					parser.WithASTTransformers(
						util.Prioritized(transformer.NewLayoutTransformer(), 100),
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
