package renderer

import (
	"fmt"
	stdhtml "html"

	"github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceDateRenderer struct{}

func NewConfluenceDateRenderer() html.Extension {
	return &ConfluenceDateRenderer{}
}

// RendererOptions implements html.Extension.
func (r *ConfluenceDateRenderer) RendererOptions(cfg *html.Config) []html.Option {
	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		parser.KindDate: nodeRenderer(r.renderDate),
	})}
}

func (r *ConfluenceDateRenderer) renderDate(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*parser.DateNode)
	// The value comes from the document, so it has to be escaped before landing
	// in an attribute: a quote in it would otherwise close datetime early and let
	// the rest be read as further attributes.
	_, _ = fmt.Fprintf(w, `<time datetime="%s" />`, stdhtml.EscapeString(string(n.Value)))
	return ast.WalkContinue, nil
}
