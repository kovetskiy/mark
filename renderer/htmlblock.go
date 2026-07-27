package renderer

import (
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceHTMLBlockRenderer struct {
	html.Config
	Stdlib      *stdlib.Lib
	Attachments attachment.Attacher
	Path        string
	ImageAlign  string
}

// NewConfluenceHTMLBlockRenderer creates a new instance of the ConfluenceHTMLBlockRenderer
func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...html.Option) renderer.NodeRenderer {
	r := &ConfluenceHTMLBlockRenderer{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		Attachments: attachments,
		Path:        path,
		ImageAlign:  imageAlign,
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceHTMLBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
}

func (r *ConfluenceHTMLBlockRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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
