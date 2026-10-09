package transformer

import (
	"io"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// HTMLRenderer renders the nodes this package puts in the tree in place of
// goldmark v1's String, TextBlock and TaskCheckBox, the way goldmark v1's own
// HTML renderer rendered those. Both compile paths register it ahead of their
// own renderers, so that the task list renderer still takes the checkbox.
type HTMLRenderer struct{}

// NewHTMLRenderer returns an HTMLRenderer.
func NewHTMLRenderer() *HTMLRenderer {
	return &HTMLRenderer{}
}

// RendererOptions implements html.Extension.
func (r *HTMLRenderer) RendererOptions(_ *html.Config) []html.Option {
	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		KindString:    html.NodeRendererFunc(r.renderString),
		KindTextBlock: html.NodeRendererFunc(r.renderTextBlock),
	})}
}

// renderString writes markup as it stands, and text decoded and then escaped.
func (r *HTMLRenderer) renderString(w io.Writer, _ []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*String)
	if n.Code {
		_, _ = w.Write(n.Value)
		return ast.WalkContinue, nil
	}

	_, _ = DecodeMarkdownTo(html.ContextTextWriter(rc), n.Value)

	return ast.WalkContinue, nil
}

// renderTextBlock writes the block's content with no element of its own, and
// a newline before whatever follows it.
func (r *HTMLRenderer) renderTextBlock(w io.Writer, _ []byte, n ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
	if !entering && n.NextSibling() != nil && n.FirstChild() != nil {
		_ = w.(util.BufWriter).WriteByte('\n')
	}

	return ast.WalkContinue, nil
}
