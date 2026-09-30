package renderer

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"strconv"

	parser "github.com/stefanfritsch/goldmark-admonitions"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// MkDocsAdmonitionAttributeFilter defines the attribute names kept on the
// blockquote an admonition falls back to when it maps to no Confluence macro.
var MkDocsAdmonitionAttributeFilter = html.GlobalAttributeFilter

// admonitionIDAttribute is the parser's internal id for an admonition, which is
// never written out.
var admonitionIDAttribute = []byte("data-admonition")

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

// MkDocsAdmonitionType is the kind of Confluence macro an admonition becomes,
// or ANone when it becomes no macro.
type MkDocsAdmonitionType int

const (
	AInfo MkDocsAdmonitionType = iota
	ANote
	AWarn
	ATip
	ANone
)

func (t MkDocsAdmonitionType) String() string {
	return []string{"info", "note", "warning", "tip", "none"}[t]
}

func ParseMkDocsAdmonitionType(node ast.Node) MkDocsAdmonitionType {
	n, ok := node.(*parser.Admonition)
	if !ok {
		return ANone
	}

	switch string(n.AdmonitionClass) {
	case "info":
		return AInfo
	case "note":
		return ANote
	case "warning":
		return AWarn
	case "tip":
		return ATip
	default:
		return ANone
	}
}

// renderMkDocsAdmonition renders an admonition node as a Confluence structured macro.
// All admonitions (including nested ones) are rendered as Confluence macros.
func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmonition(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	admonitionType := ParseMkDocsAdmonitionType(node)

	if entering && admonitionType != ANone {
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
	if !entering && admonitionType != ANone {
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
		// The parser tags every admonition with a data-admonition id of its
		// own for bookkeeping: 24 random letters, or the nesting level once it
		// sees the block close. Written out, it made two compiles of the same
		// document differ, so --changes-only republished the page every run.
		// It means nothing to Confluence, so it stays behind.
		dropAttribute(n, admonitionIDAttribute)
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

// dropAttribute removes one attribute from a node, keeping the rest in order.
func dropAttribute(n ast.Node, name []byte) {
	attributes := n.Attributes()
	n.RemoveAttributes()
	for _, attribute := range attributes {
		if !bytes.Equal(attribute.Name, name) {
			n.SetAttribute(attribute.Name, attribute.Value)
		}
	}
}
