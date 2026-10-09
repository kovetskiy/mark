// Package goldmarktest builds the parser and renderer pair the tests of the
// goldmark extensions in this repository convert Markdown with.
//
// goldmark v2 has no goldmark.Markdown: a parser.Parser and an html.Renderer
// are built and called separately. The tests were written against the one
// object, and keep reading as they did through this one.
package goldmarktest

import (
	"io"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// Markdown is a parser and a renderer configured together.
type Markdown struct {
	parserOptions   []parser.Option
	rendererOptions []html.Option
}

// Option configures a Markdown.
type Option func(*Markdown)

// New returns a Markdown with the given options.
//
// It imports nothing from this repository, so that the tests of any package
// here can use it.
func New(opts ...Option) *Markdown {
	m := &Markdown{}
	for _, opt := range opts {
		opt(m)
	}

	return m
}

// WithParserOptions adds parser options.
func WithParserOptions(opts ...parser.Option) Option {
	return func(m *Markdown) {
		m.parserOptions = append(m.parserOptions, opts...)
	}
}

// WithRendererOptions adds renderer options.
func WithRendererOptions(opts ...html.Option) Option {
	return func(m *Markdown) {
		m.rendererOptions = append(m.rendererOptions, opts...)
	}
}

// WithExtensions adds extensions, each to the parser or the renderer or both,
// according to which of the two it implements.
func WithExtensions(extensions ...any) Option {
	return func(m *Markdown) {
		for _, extension := range extensions {
			if e, ok := extension.(parser.Extension); ok {
				m.parserOptions = append(m.parserOptions, parser.WithExtensions(e))
			}
			if e, ok := extension.(html.Extension); ok {
				m.rendererOptions = append(m.rendererOptions, html.WithExtensions(e))
			}
		}
	}
}

// Parse parses source.
func (m *Markdown) Parse(source []byte) ast.Node {
	return parser.New(m.parserOptions...).Parse(source)
}

// Convert parses source and renders it to w.
func (m *Markdown) Convert(source []byte, w io.Writer) error {
	return html.New(m.rendererOptions...).Render(w, source, m.Parse(source))
}
