package transformer

import (
	"io"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// HTMLRenderer renders this package's String and TextBlock nodes the way
// goldmark v1 rendered its own. Both compile paths register it.
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
// a newline before whatever follows it. A task's block counts its checkbox as
// content, as v1's tree held it as a child.
func (r *HTMLRenderer) renderTextBlock(w io.Writer, _ []byte, n ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
	if !entering && n.NextSibling() != nil && (n.FirstChild() != nil || OpensTask(n)) {
		_ = w.(util.BufWriter).WriteByte('\n')
	}

	return ast.WalkContinue, nil
}

// OpensTask reports whether block is the first block of a task list item,
// which goldmark's task list parser opens with the checkbox.
func OpensTask(block ast.Node) bool {
	item := block.Parent()

	return item != nil && item.FirstChild() == block && extension.IsTask(item)
}
