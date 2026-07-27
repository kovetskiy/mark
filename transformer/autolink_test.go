package transformer_test

import (
	"bytes"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

func TestAutoLinkTransformer(t *testing.T) {
	gm := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
		),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(transformer.NewAutoLinkTransformer(), 110),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(
				util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
			),
		),
	)

	markdown := []byte("Check out https://example.com and user@example.com for info.")
	var buf bytes.Buffer
	err := gm.Convert(markdown, &buf)
	assert.NoError(t, err)
	output := buf.String()

	assert.Contains(t, output, `href="https://example.com" data-card-appearance="inline"`)
	assert.Contains(t, output, `href="mailto:user@example.com" data-card-appearance="inline"`)
}
