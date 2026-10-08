package renderer_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// render compiles source with the given Confluence renderers registered, and
// returns what they wrote.
//
// The renderers go in as html.Extensions given to html.New, which is what
// markdown/markdown.go does and what makes them win: html.New registers
// goldmark's own renderers for every core node kind first, and of two
// registrations for one kind the later is the one that renders (AGENTS.md,
// invariant 4). Registering them any other way here could quietly test
// goldmark's output instead of this repo's.
//
// The parser is given the transformers both compile paths run to give the tree
// the shape these renderers read.
func render(t *testing.T, source string, nodeRenderers []html.Extension, parserOpts ...parser.Option) string {
	t.Helper()

	return renderExtended(t, source, nil, nodeRenderers, parserOpts...)
}

// renderExtended is render for a renderer whose nodes exist only once a
// goldmark extension has parsed them, footnotes being the one in this package.
func renderExtended(t *testing.T, source string, extensions []parser.Extension, nodeRenderers []html.Extension, parserOpts ...parser.Option) string {
	t.Helper()

	opts := []parser.Option{
		parser.WithExtensions(extensions...),
		parser.WithASTTransformers(ctransformer.ShapeTransformers()...),
	}
	p := parser.New(append(opts, parserOpts...)...)

	r := html.New(
		html.WithUnsafe(),
		html.WithXHTML(),
		html.WithExtensions(ctransformer.NewHTMLRenderer()),
		html.WithExtensions(nodeRenderers...),
	)

	src := []byte(source)

	var buf bytes.Buffer
	require.NoError(t, r.Render(&buf, src, p.Parse(src)))

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
