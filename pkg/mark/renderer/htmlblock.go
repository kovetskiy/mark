package renderer

import (
	"strings"

	"github.com/kovetskiy/mark/pkg/mark/stdlib"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceHTMLBlockRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceHTMLBlockRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceHTMLBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
}

func (r *ConfluenceHTMLBlockRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return r.goldmarkRenderHTMLBlock(w, source, node, entering)
	}

	n := node.(*ast.HTMLBlock)
	l := n.Lines().Len()
	for i := 0; i < l; i++ {
		line := n.Lines().At(i)

		switch strings.Trim(string(line.Value(source)), "\n") {
		case "<!-- ac:layout -->":
			_, _ = w.WriteString("<ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout end -->":
			_, _ = w.WriteString("</ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:single -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"single\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_equal -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_equal\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_left_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_left_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_right_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_right_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three_with_sidebars -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three_with_sidebars\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section end -->":
			_, _ = w.WriteString("</ac:layout-section>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-cell -->":
			_, _ = w.WriteString("<ac:layout-cell>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-cell end -->":
			_, _ = w.WriteString("</ac:layout-cell>\n")
			return ast.WalkContinue, nil

		}
	}
	return r.goldmarkRenderHTMLBlock(w, source, node, entering)

}

func (r *ConfluenceHTMLBlockRenderer) goldmarkRenderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.HTMLBlock)
	if entering {
		if r.Unsafe {
			l := n.Lines().Len()
			for i := 0; i < l; i++ {
				line := n.Lines().At(i)
				r.Writer.SecureWrite(w, line.Value(source))
			}
		} else {
			_, _ = w.WriteString("<!-- raw HTML omitted -->\n")
		}
	} else {
		if n.HasClosure() {
			if r.Unsafe {
				closure := n.ClosureLine
				r.Writer.SecureWrite(w, closure.Value(source))
			} else {
				_, _ = w.WriteString("<!-- raw HTML omitted -->\n")
			}
		}
	}
	return ast.WalkContinue, nil
}
