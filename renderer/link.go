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
	n := node.(*ast.Link)
	if len(n.Destination) >= 3 && string(n.Destination[0:3]) == "ac:" {
		if entering {
			_, err := writer.Write([]byte("<ac:link><ri:page ri:content-title=\""))
			if err != nil {
				return ast.WalkStop, err
			}

			if len(string(n.Destination)) < 4 {
				//nolint:staticcheck
				_, err := writer.Write(node.Text(source))
				if err != nil {
					return ast.WalkStop, err
				}
			} else {
				_, err := writer.Write(n.Destination[3:])
				if err != nil {
					return ast.WalkStop, err
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				return ast.WalkStop, err
			}

			//nolint:staticcheck
			_, err = writer.Write(node.Text(source))
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
	if entering {
		_, _ = writer.WriteString("<a href=\"")
		if r.Unsafe || !html.IsDangerousURL(n.Destination) {
			_, _ = writer.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
		}
		_ = writer.WriteByte('"')
		if n.Title != nil {
			_, _ = writer.WriteString(` title="`)
			r.Writer.Write(writer, n.Title)
			_ = writer.WriteByte('"')
		}
		if n.Attributes() != nil {
			html.RenderAttributes(writer, n, html.LinkAttributeFilter)
		}
		_ = writer.WriteByte('>')
	} else {
		_, _ = writer.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}
