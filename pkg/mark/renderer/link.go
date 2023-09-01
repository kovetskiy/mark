package renderer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceLinkRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceLinkRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceLinkRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceLinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
}

// renderLink renders links specifically for confluence
func (r *ConfluenceLinkRenderer) renderLink(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if string(node.(*ast.Link).Destination[0:3]) == "ac:" {
		if entering {
			_, err := writer.Write([]byte("<ac:link><ri:page ri:content-title=\""))
			if err != nil {
				return ast.WalkStop, err
			}

			if len(node.(*ast.Link).Destination) < 4 {
				_, err := writer.Write(node.FirstChild().Text(source))
				if err != nil {
					return ast.WalkStop, err
				}
			} else {
				_, err := writer.Write(node.(*ast.Link).Destination[3:])
				if err != nil {
					return ast.WalkStop, err
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				return ast.WalkStop, err
			}

			_, err = writer.Write(node.FirstChild().Text(source))
			if err != nil {
				return ast.WalkStop, err
			}

			_, err = writer.Write([]byte("]]></ac:plain-text-link-body></ac:link>"))
			if err != nil {
				return ast.WalkStop, err
			}
		}
		return ast.WalkSkipChildren, nil
	}
	return r.goldmarkRenderLink(writer, source, node, entering)
}

// goldmarkRenderLink is the default renderLink implementation from https://github.com/yuin/goldmark/blob/9d6f314b99ca23037c93d76f248be7b37de6220a/renderer/html/html.go#L552
func (r *ConfluenceLinkRenderer) goldmarkRenderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if entering {
		_, _ = w.WriteString("<a href=\"")
		if r.Unsafe || !html.IsDangerousURL(n.Destination) {
			_, _ = w.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
		}
		_ = w.WriteByte('"')
		if n.Title != nil {
			_, _ = w.WriteString(` title="`)
			r.Writer.Write(w, n.Title)
			_ = w.WriteByte('"')
		}
		if n.Attributes() != nil {
			html.RenderAttributes(w, n, html.LinkAttributeFilter)
		}
		_ = w.WriteByte('>')
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}
