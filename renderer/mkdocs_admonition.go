package renderer

import (
	"fmt"
	stdhtml "html"
	"strconv"

	"github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// MkDocsAdmonitionAttributeFilter defines the attribute names kept on the
// blockquote an admonition falls back to when it maps to no Confluence macro.
var MkDocsAdmonitionAttributeFilter = html.GlobalAttributeFilter

// ConfluenceMkDocsAdmonitionRenderer renders MkDocs admonitions as Confluence
// storage format.
type ConfluenceMkDocsAdmonitionRenderer struct {
	htmlOptions
}

// NewConfluenceMkDocsAdmonitionRenderer creates a new instance of the ConfluenceMkDocsAdmonitionRenderer.
func NewConfluenceMkDocsAdmonitionRenderer(opts ...html.Option) html.Extension {
	r := &ConfluenceMkDocsAdmonitionRenderer{}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceMkDocsAdmonitionRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		parser.KindAdmonition: nodeRenderer(r.renderMkDocsAdmonition),
	})}
}

// ParseMkDocsAdmonitionType returns the macro an admonition node is published
// as, or AdmonitionNone for a class Confluence has no macro for, or a node that
// is not an admonition.
func ParseMkDocsAdmonitionType(node ast.Node) AdmonitionType {
	n, ok := node.(*parser.Admonition)
	if !ok {
		return AdmonitionNone
	}

	if t, ok := mkDocsAdmonitionTypes[string(n.AdmonitionClass)]; ok {
		return t
	}
	return AdmonitionNone
}

// renderMkDocsAdmonition renders an admonition node as a Confluence structured macro.
// All admonitions (including nested ones) are rendered as Confluence macros.
func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmonition(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	admonitionType := ParseMkDocsAdmonitionType(node)

	if entering && admonitionType != AdmonitionNone {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", admonitionType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}

		title, _ := strconv.Unquote(string(n.Title))
		if title != "" {
			titleHTML := fmt.Sprintf("<p><strong>%s</strong></p>\n", stdhtml.EscapeString(title))
			if _, err := writer.Write([]byte(titleHTML)); err != nil {
				return ast.WalkStop, err
			}
		}

		return ast.WalkContinue, nil
	}
	if !entering && admonitionType != AdmonitionNone {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	return r.renderMkDocsAdmon(writer, source, node, entering)
}

func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmon(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	if entering {
		if len(n.Attributes()) > 0 {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, source, n, MkDocsAdmonitionAttributeFilter, nil)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
