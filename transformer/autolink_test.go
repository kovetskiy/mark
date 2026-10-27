package transformer_test

import (
	"bytes"
	"testing"

	"github.com/kovetskiy/mark/v16/internal/goldmarktest"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

func TestAutoLinkTransformer(t *testing.T) {
	gm := goldmarktest.New(
		goldmarktest.WithExtensions(
			extension.GFMParser, extension.GFMHTMLRenderer,
		),
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](transformer.NewAutoLinkTransformer(), 110),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(transformer.NewHTMLRenderer()),
			html.WithUnsafe(),
			html.WithExtensions(
				crenderer.NewConfluenceLinkRenderer(nil, nil, "", false),
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
