package renderer

import (
	"fmt"

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
	_, _ = fmt.Fprintf(w, `<time datetime="%s" />`, string(n.Value))
	return ast.WalkContinue, nil
}
