package renderer

import (
	"github.com/kovetskiy/mark/v16/stdlib"
	stdhtml "html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceLinkRenderer struct {
	html.Config
	Stdlib *stdlib.Lib
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceLinkRenderer(lib *stdlib.Lib, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceLinkRenderer{
		Config: html.NewConfig(),
		Stdlib: lib,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceLinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
}

// renderLink renders links specifically for confluence
func (r *ConfluenceLinkRenderer) renderLink(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)

	// A link to an anchor on this page. The HTML idiom -- href="#X" against an
	// id="X" on the heading -- renders and does nothing when clicked, because
	// Confluence keeps no id on a heading and generates its own from the
	// element's text. ac:link with ac:anchor and no ri:page is the storage
	// format's own way of saying "somewhere on this page", and is what the
	// footnote renderers have always emitted.
	if anchor, found := strings.CutPrefix(string(n.Destination), "#"); found && anchor != "" && r.Stdlib != nil {
		if entering {
			err := r.Stdlib.Templates.ExecuteTemplate(writer, "ac:link:anchor", struct {
				Anchor string
			}{anchor})
			if err != nil {
				return ast.WalkStop, err
			}

			return ast.WalkContinue, nil
		}

		if _, err := writer.WriteString("</ac:link-body></ac:link>"); err != nil {
			return ast.WalkStop, err
		}

		return ast.WalkContinue, nil
	}

	if len(n.Destination) >= 3 && string(n.Destination[0:3]) == "ac:" {
		if entering {
			_, err := writer.Write([]byte("<ac:link><ri:page ri:content-title=\""))
			if err != nil {
				return ast.WalkStop, err
			}

			// The page title lands in an XML attribute, so it has to be escaped:
			// an unescaped "&" makes the body malformed and a quote closes the
			// attribute early, letting document content inject further attributes.
			if len(string(n.Destination)) < 4 {
				//nolint:staticcheck
				_, err := writer.WriteString(xmlAttrEscape(string(node.Text(source))))
				if err != nil {
					return ast.WalkStop, err
				}
			} else {
				_, err := writer.WriteString(xmlAttrEscape(string(n.Destination[3:])))
				if err != nil {
					return ast.WalkStop, err
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				return ast.WalkStop, err
			}

			// A "]]>" in the link text would terminate the CDATA section early;
			// splitting it across two sections is the only way to escape it.
			//nolint:staticcheck
			_, err = writer.WriteString(cdataEscape(string(node.Text(source))))
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

// xmlAttrEscape makes a document-derived string safe to interpolate into an XML
// attribute value.
func xmlAttrEscape(s string) string {
	return stdhtml.EscapeString(s)
}

// cdataEscape splits any "]]>" in s across two CDATA sections, which is the only
// way to represent that sequence inside one. Mirrors the stdlib "cdata" template
// function used by the ac:code and ac:plantuml macros.
func cdataEscape(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]><![CDATA[]]]]><![CDATA[>")
}
