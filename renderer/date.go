package renderer

import (
	"fmt"
	"html"

	"github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type ConfluenceDateRenderer struct{}

func NewConfluenceDateRenderer() renderer.NodeRenderer {
	return &ConfluenceDateRenderer{}
}

func (r *ConfluenceDateRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(parser.KindDate, r.renderDate)
}

func (r *ConfluenceDateRenderer) renderDate(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*parser.DateNode)
	// The value comes from the document, so it has to be escaped before landing
	// in an attribute: a quote in it would otherwise close datetime early and let
	// the rest be read as further attributes.
	_, _ = fmt.Fprintf(w, `<time datetime="%s" />`, html.EscapeString(string(n.Value)))
	return ast.WalkContinue, nil
}
