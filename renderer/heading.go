package renderer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceHeadingRenderer struct {
	html.Config
	DropFirstH1 bool
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceHeadingRenderer(dropFirstH1 bool, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceHeadingRenderer{
		Config:      html.NewConfig(),
		DropFirstH1: dropFirstH1,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceHeadingRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, r.renderHeading)
}

func (r *ConfluenceHeadingRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)

	// If this is the first h1 heading of the document and we want to drop it, let's not render it at all.
	if n.Level == 1 && r.DropFirstH1 {
		if !entering {
			r.DropFirstH1 = false
		}
		return ast.WalkSkipChildren, nil
	}

	return r.goldmarkRenderHeading(w, source, node, entering)
}

func (r *ConfluenceHeadingRenderer) goldmarkRenderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		_, _ = w.WriteString("<h")
		_ = w.WriteByte("0123456"[n.Level])
		if n.Attributes() != nil {
			html.RenderAttributes(w, node, html.HeadingAttributeFilter)
		}
		_ = w.WriteByte('>')
	} else {
		_, _ = w.WriteString("</h")
		_ = w.WriteByte("0123456"[n.Level])
		_, _ = w.WriteString(">\n")
	}
	return ast.WalkContinue, nil
}
