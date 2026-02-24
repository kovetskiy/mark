package renderer

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	cparser "github.com/kovetskiy/mark/parser"
)

type ConfluenceMathLatexRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceMathLatexRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceMathLatexRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceMathLatexRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(cparser.KindMathLatexInline, r.renderInline)
	reg.Register(cparser.KindMathLatexBlock, r.renderBlock)
}

func (r *ConfluenceMathLatexRenderer) renderInline(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		node := n.(*cparser.MathLatexInline)
		quoteType := "ppl mathjax inline macro"

		w.WriteString(fmt.Sprintf("<ac:structured-macro ac:name=\"%s\">", quoteType))
		w.WriteString("<ac:parameter ac:name=\"equation\">")
		w.Write(node.Equation)
		w.WriteString("</ac:parameter>")
		w.WriteString("</ac:structured-macro>")
	}
	return ast.WalkContinue, nil
}

func (r *ConfluenceMathLatexRenderer) renderBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		node := n.(*cparser.MathLatexBlock)
		quoteType := "ppl mathjax block macro"

		w.WriteString(fmt.Sprintf("<ac:structured-macro ac:name=\"%s\">", quoteType))
		w.WriteString("<ac:plain-text-body><![CDATA[")
		w.Write(node.Equation)
		w.WriteString("]]></ac:plain-text-body>")
		w.WriteString("</ac:structured-macro>")
	}

	return ast.WalkContinue, nil
}
