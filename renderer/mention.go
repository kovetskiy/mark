package renderer

import (
	"github.com/kovetskiy/mark/parser"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type ConfluenceMentionRenderer struct {
	Stdlib *stdlib.Lib
}

func NewConfluenceMentionRenderer(stdlib *stdlib.Lib) renderer.NodeRenderer {
	return &ConfluenceMentionRenderer{
		Stdlib: stdlib,
	}
}

func (r *ConfluenceMentionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(parser.KindMention, r.renderMention)
}

func (r *ConfluenceMentionRenderer) renderMention(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*parser.Mention)

	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:link:user", struct {
		Name string
	}{
		Name: string(n.Name),
	})
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}
