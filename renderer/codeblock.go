package renderer

import (
	"strings"

	"github.com/kovetskiy/mark/v16/stdlib"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceCodeBlockRenderer struct {
	htmlOptions

	Stdlib *stdlib.Lib
}

// NewConfluenceCodeBlockRenderer creates a renderer for indented code blocks.
func NewConfluenceCodeBlockRenderer(stdlib *stdlib.Lib, opts ...html.Option) html.Extension {
	r := &ConfluenceCodeBlockRenderer{
		Stdlib: stdlib,
	}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceCodeBlockRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindCodeBlock: nodeRenderer(r.renderCodeBlock),
	})}
}

// renderCodeBlock renders a CodeBlock
func (r *ConfluenceCodeBlockRenderer) renderCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false
	lang := ""
	title := ""

	lval := node.(*ast.CodeBlock).Value.Bytes(source)
	err := r.Stdlib.Templates.ExecuteTemplate(
		writer,
		"ac:code",
		struct {
			Language    string
			Collapse    bool
			Title       string
			Theme       string
			Linenumbers bool
			Firstline   int
			Text        string
		}{
			lang,
			collapse,
			title,
			theme,
			linenumbers,
			firstline,
			strings.TrimSuffix(string(lval), "\n"),
		},
	)
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}
