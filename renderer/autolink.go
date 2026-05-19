package renderer

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// ConfluenceAutoLinkRenderer renders ast.KindAutoLink (bare URLs in markdown,
// goldmark-autodetected) as anchor tags carrying the
// `data-card-appearance="inline"` attribute. Confluence Cloud uses that hint
// to render the URL as an inline smart card (page mention / Jira / GitHub /
// etc.) instead of a plain hyperlink.
//
// Markdown-explicit links (`[text](url)`) are left to the existing
// ConfluenceLinkRenderer so that author-chosen display text is preserved
// unchanged as a regular hyperlink. Only bare/auto-detected URLs become
// inline cards.
type ConfluenceAutoLinkRenderer struct {
	html.Config
}

// NewConfluenceAutoLinkRenderer creates an instance of the autolink renderer.
func NewConfluenceAutoLinkRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &ConfluenceAutoLinkRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r *ConfluenceAutoLinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
}

func (r *ConfluenceAutoLinkRenderer) renderAutoLink(
	w util.BufWriter, source []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	n := node.(*ast.AutoLink)
	if !entering {
		return ast.WalkContinue, nil
	}
	url := util.URLEscape(n.URL(source), false)
	label := n.Label(source)
	_, _ = w.WriteString(`<a href="`)
	if n.AutoLinkType == ast.AutoLinkEmail && !bytes.HasPrefix(bytes.ToLower(url), []byte("mailto:")) {
		_, _ = w.WriteString("mailto:")
	}
	if r.Unsafe || !html.IsDangerousURL(url) {
		_, _ = w.Write(util.EscapeHTML(url))
	}
	_, _ = w.WriteString(`" data-card-appearance="inline">`)
	_, _ = w.Write(util.EscapeHTML(label))
	_, _ = w.WriteString(`</a>`)
	return ast.WalkContinue, nil
}
