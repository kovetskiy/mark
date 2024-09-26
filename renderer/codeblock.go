package renderer

import (
	"strings"

	"github.com/kovetskiy/mark/stdlib"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceCodeBlockRenderer struct {
	html.Config
	Stdlib *stdlib.Lib
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceCodeBlockRenderer(stdlib *stdlib.Lib, path string, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceCodeBlockRenderer{
		Config: html.NewConfig(),
		Stdlib: stdlib,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceCodeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
}

// renderCodeBlock renders a CodeBlock
func (r *ConfluenceCodeBlockRenderer) renderCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false
	lang := ""
	title := ""

	var lval []byte

	lines := node.Lines().Len()
	for i := 0; i < lines; i++ {
		line := node.Lines().At(i)
		lval = append(lval, line.Value(source)...)
	}
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
