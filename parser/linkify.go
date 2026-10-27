package parser

import (
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// LinkifyInEmphasis is an inline parser that never parses anything. It is
// there for its trigger alone.
//
// goldmark's emphasis fast path takes "**https://x.com**" as plain text when no
// byte between the delimiters triggers an inline parser, without trying
// linkify at the start of the content as the full parse would. Every URL,
// www. link and address linkify matches holds a '.', so triggering on it
// sends such emphasis through the full parse, where linkify finds the link.
func LinkifyInEmphasis() util.PrioritizedValue[parser.InlineParser] {
	return util.Prioritized[parser.InlineParser](dotTrigger{}, 1000)
}

type dotTrigger struct{}

func (dotTrigger) Trigger() []byte { return []byte{'.'} }

func (dotTrigger) Parse(ast.Node, text.Reader, parser.Context) ast.Node { return nil }
