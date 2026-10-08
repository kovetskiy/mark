package renderer

import (
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceHTMLBlockRenderer struct {
	html.Config

	// options are the html options the constructor was given, which apply
	// on top of the ones the renderer is registered with.
	options     []html.Option
	Stdlib      *stdlib.Lib
	Attachments attachment.Attacher
	Path        string
	ImageAlign  string
}

// NewConfluenceHTMLBlockRenderer creates a new instance of the ConfluenceHTMLBlockRenderer
func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...html.Option) html.Extension {
	r := &ConfluenceHTMLBlockRenderer{
		Stdlib:      stdlib,
		Attachments: attachments,
		Path:        path,
		ImageAlign:  imageAlign,
	}
	r.options = opts
	r.Config = withOptions(html.Config{}.Default(), opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceHTMLBlockRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.Config = withOptions(*cfg, r.options)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindHTMLBlock: contextNodeRenderer(r.renderHTMLBlock),
	})}
}

func (r *ConfluenceHTMLBlockRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	return r.goldmarkRenderHTMLBlock(w, source, node, entering, rc)
}

// goldmarkRenderHTMLBlock is goldmark's own HTML block renderer. The block's
// closing line is among its others in goldmark v2, so the whole of it is
// written on the way in, through the writer that does what v1's SecureWrite
// did: the bytes as they are, a NUL replaced.
func (r *ConfluenceHTMLBlockRenderer) goldmarkRenderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	n := node.(*ast.HTMLBlock)
	if entering {
		if r.Unsafe {
			_, _ = n.Value.WriteTo(html.ContextHTMLWriter(rc), source)
		} else {
			_, _ = w.WriteString("<!-- raw HTML omitted -->\n")
		}
	}
	return ast.WalkContinue, nil
}
