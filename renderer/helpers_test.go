package renderer_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// render compiles source with the given Confluence renderers registered, and
// returns what they wrote.
//
// The renderers go in at 100, which is what markdown/markdown.go uses and what
// makes them win: goldmark's own html.Renderer registers at 1000 and claims
// every core node kind, and the *smaller* number is the one that ends up
// rendering (AGENTS.md, invariant 4). Registering at a larger number here would
// quietly test goldmark's output instead of this repo's.
func render(t *testing.T, source string, nodeRenderers []renderer.NodeRenderer, parserOpts ...parser.Option) string {
	t.Helper()

	return renderExtended(t, source, nil, nodeRenderers, parserOpts...)
}

// renderExtended is render for a renderer whose nodes exist only once a
// goldmark extension has parsed them, footnotes being the one in this package.
func renderExtended(t *testing.T, source string, extensions []goldmark.Extender, nodeRenderers []renderer.NodeRenderer, parserOpts ...parser.Option) string {
	t.Helper()

	prioritized := make([]util.PrioritizedValue, 0, len(nodeRenderers))
	for _, nodeRenderer := range nodeRenderers {
		prioritized = append(prioritized, util.Prioritized(nodeRenderer, 100))
	}

	converter := goldmark.New(
		goldmark.WithExtensions(extensions...),
		goldmark.WithParserOptions(parserOpts...),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(prioritized...),
			html.WithUnsafe(),
			html.WithXHTML(),
		),
	)

	var buf bytes.Buffer
	require.NoError(t, converter.Convert([]byte(source), &buf))

	return buf.String()
}

// newStdlib builds the template set with no Confluence API behind it, which is
// what every renderer that does not resolve a user needs.
func newStdlib(t *testing.T) *stdlib.Lib {
	t.Helper()

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	return lib
}

// assertWellFormed parses a body the way Confluence does.
//
// Confluence answers a body that is not well-formed with BadRequestException
// and rejects the whole page, so an unbalanced tag here is not one broken
// element but a document that never uploads. Every renderer that emits a macro
// pair is checked with this rather than by eye.
func assertWellFormed(t *testing.T, body string) {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(`<root xmlns:ac="ac" xmlns:ri="ri">` + body + `</root>`))
	decoder.Strict = true
	decoder.Entity = xml.HTMLEntity

	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if !assert.NoError(t, err, "storage format must be well-formed XML") {
			return
		}
	}
}
