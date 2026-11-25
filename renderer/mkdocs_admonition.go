package renderer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/parser"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
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
// as, or AdmonitionNone for a class that is not an admonition word, or a node
// that is not an admonition.
func ParseMkDocsAdmonitionType(node ast.Node) AdmonitionType {
	n, ok := node.(*parser.Admonition)
	if !ok {
		return AdmonitionNone
	}

	return mkDocsAdmonitionType(string(n.AdmonitionClass))
}

// renderMkDocsAdmonition renders an admonition node as a Confluence structured macro.
// All admonitions (including nested ones) are rendered as Confluence macros.
//
// Confluence's expand macro cannot start open, so only '???' is wrapped;
// '???+' reads as '!!!'. The expand goes around whatever the block renders
// as, a class with no panel included.
func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmonition(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)

	if entering && n.Collapsed {
		expand := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">` +
			stdlib.XMLEscape(collapsedAdmonitionTitle(n)) +
			"</ac:parameter><ac:rich-text-body>\n\n"
		if _, err := writer.WriteString(expand); err != nil {
			return ast.WalkStop, err
		}
	}

	if status, err := r.renderPanel(writer, source, n, entering, rc); err != nil {
		return status, err
	}

	if !entering && n.Collapsed {
		if _, err := writer.WriteString("\n</ac:rich-text-body></ac:structured-macro>\n"); err != nil {
			return ast.WalkStop, err
		}
	}

	return ast.WalkContinue, nil
}

// renderPanel renders the block itself: the macro of its type, or a
// blockquote for a class with none.
func (r *ConfluenceMkDocsAdmonitionRenderer) renderPanel(writer util.BufWriter, source []byte, n *parser.Admonition, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	admonitionType := ParseMkDocsAdmonitionType(n)

	if entering && admonitionType != AdmonitionNone {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", admonitionType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}

		if title := admonitionTitle(n); title != "" {
			titleHTML := fmt.Sprintf("<p><strong>%s</strong></p>\n", stdlib.XMLEscape(title))
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
	return r.renderMkDocsAdmon(writer, source, n, entering, rc)
}

// admonitionTitle is the quoted title of an opening line. MkDocs titles are
// literal text: only \" and \\ are escapes, as where the parser finds the
// closing quote.
func admonitionTitle(n *parser.Admonition) string {
	raw := string(n.Title)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return ""
	}

	return admonitionTitleEscapes.Replace(raw[1 : len(raw)-1])
}

var admonitionTitleEscapes = strings.NewReplacer(`\"`, `"`, `\\`, `\`)

// collapsedAdmonitionTitle is the title of the expand macro around a '???'
// admonition: the quoted title, or the class with its first letter capitalised
// when there is none.
func collapsedAdmonitionTitle(n *parser.Admonition) string {
	if title := admonitionTitle(n); title != "" {
		return title
	}

	class := string(n.AdmonitionClass)
	first, size := utf8.DecodeRuneInString(class)
	return string(unicode.ToUpper(first)) + class[size:]
}

func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmon(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	if entering {
		if len(n.Attributes()) > 0 {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, source, n, MkDocsAdmonitionAttributeFilter, rc)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
