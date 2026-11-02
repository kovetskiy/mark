package renderer

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v16/parser"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// MkDocsAdmonitionAttributeFilter defines the attribute names kept on the
// blockquote an admonition falls back to when it maps to no Confluence macro.
var MkDocsAdmonitionAttributeFilter = html.GlobalAttributeFilter

// ConfluenceMkDocsAdmonitionRenderer renders MkDocs admonitions as Confluence
// storage format.
type ConfluenceMkDocsAdmonitionRenderer struct {
	html.Config
}

// NewConfluenceMkDocsAdmonitionRenderer creates a new instance of the ConfluenceMkDocsAdmonitionRenderer.
func NewConfluenceMkDocsAdmonitionRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &ConfluenceMkDocsAdmonitionRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs.
func (r *ConfluenceMkDocsAdmonitionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(parser.KindAdmonition, r.renderMkDocsAdmonition)
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
		if n.Collapsed {
			// Confluence's expand macro cannot start open, so only '???' is
			// wrapped; '???+' reads as '!!!'. The panel inside is the one '!!!'
			// makes, title paragraph and all.
			expand := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">` +
				stdlib.XMLEscape(collapsedAdmonitionTitle(n)) +
				"</ac:parameter><ac:rich-text-body>\n\n"
			if _, err := writer.Write([]byte(expand)); err != nil {
				return ast.WalkStop, err
			}
		}

		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", admonitionType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}

		title, _ := strconv.Unquote(string(n.Title))
		if title != "" {
			titleHTML := fmt.Sprintf("<p><strong>%s</strong></p>\n", stdlib.XMLEscape(title))
			if _, err := writer.Write([]byte(titleHTML)); err != nil {
				return ast.WalkStop, err
			}
		}

		return ast.WalkContinue, nil
	}
	if !entering && admonitionType != AdmonitionNone {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if n.Collapsed {
			suffix += "\n</ac:rich-text-body></ac:structured-macro>\n"
		}
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	return r.renderMkDocsAdmon(writer, source, node, entering)
}

// collapsedAdmonitionTitle is the title of the expand macro around a '???'
// admonition: the quoted title, or the class with its first letter capitalised
// when there is none.
func collapsedAdmonitionTitle(n *parser.Admonition) string {
	if title, _ := strconv.Unquote(string(n.Title)); title != "" {
		return title
	}

	class := string(n.AdmonitionClass)
	first, size := utf8.DecodeRuneInString(class)
	return string(unicode.ToUpper(first)) + class[size:]
}

func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmon(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	if entering {
		if len(n.Attributes()) > 0 {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, n, MkDocsAdmonitionAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
