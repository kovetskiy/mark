package renderer

import (
	"github.com/kovetskiy/mark/v16/parser"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceMentionRenderer struct {
	Stdlib *stdlib.Lib
}

func NewConfluenceMentionRenderer(stdlib *stdlib.Lib) html.Extension {
	return &ConfluenceMentionRenderer{
		Stdlib: stdlib,
	}
}

// RendererOptions implements html.Extension.
func (r *ConfluenceMentionRenderer) RendererOptions(cfg *html.Config) []html.Option {
	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		parser.KindMention: nodeRenderer(r.renderMention),
	})}
}

func (r *ConfluenceMentionRenderer) renderMention(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
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
