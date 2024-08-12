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

func (r *ConfluenceParagraphRenderer) renderParagraph(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if n.FirstChild().Kind() != ast.KindRawHTML {
			if n.Attributes() != nil {
				_, _ = w.WriteString("<p")
				html.RenderAttributes(w, n, html.ParagraphAttributeFilter)
				_ = w.WriteByte('>')
			} else {
				_, _ = w.WriteString("<p>")
			}
		}
	} else {
		if n.FirstChild().Kind() != ast.KindRawHTML {
			_, _ = w.WriteString("</p>")
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}
