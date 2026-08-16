package renderer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceParagraphRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceParagraphRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceParagraphRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceParagraphRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindParagraph, r.renderParagraph)
}

// startsWithRawMarkup reports whether a paragraph opens with markup that has to
// reach Confluence as written.
//
// A paragraph that is really the opening tag of a macro must not be wrapped in
// <p>: the tag stays open across the blocks that follow, and a <p> closed
// around it produces markup Confluence rejects.
//
// Two shapes mean the same thing here. A tag parsed straight out of the
// document is an ast.RawHTML. One that arrived by expanding a macro or an
// include is an ast.String carrying its own bytes, because the expansion was
// parsed from a different buffer than the document being rendered and a segment
// into that buffer would be meaningless. Both are raw and code -- goldmark's
// typographer also produces code strings, for em dashes and quotes, but never
// marks them raw, so the pair is unambiguous.
func startsWithRawMarkup(n ast.Node) bool {
	if n == nil {
		return false
	}

	if n.Kind() == ast.KindRawHTML {
		return true
	}

	str, ok := n.(*ast.String)

	return ok && str.IsRaw() && str.IsCode()
}

func (r *ConfluenceParagraphRenderer) renderParagraph(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	firstChild := n.FirstChild()
	if entering {
		if !startsWithRawMarkup(firstChild) {
			if n.Attributes() != nil {
				_, _ = w.WriteString("<p")
				html.RenderAttributes(w, n, html.ParagraphAttributeFilter)
				_ = w.WriteByte('>')
			} else {
				_, _ = w.WriteString("<p>")
			}
		}
	} else {
		if !startsWithRawMarkup(firstChild) {
			_, _ = w.WriteString("</p>")
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}
