package renderer

import (
	"fmt"
	"strconv"

	parser "github.com/stefanfritsch/goldmark-admonitions"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// HeadingAttributeFilter defines attribute names which heading elements can have
var MkDocsAdmonitionAttributeFilter = html.GlobalAttributeFilter

// A Renderer struct is an implementation of renderer.NodeRenderer that renders
// nodes as (X)HTML.
type ConfluenceMkDocsAdmonitionRenderer struct {
	html.Config
}

// NewConfluenceMkDocsAdmonitionRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceMkDocsAdmonitionRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceMkDocsAdmonitionRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs.
func (r *ConfluenceMkDocsAdmonitionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(parser.KindAdmonition, r.renderMkDocsAdmonition)
}

// Define MkDocsAdmonitionType enum
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
			titleHTML := fmt.Sprintf("<p><strong>%s</strong></p>\n", title)
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
		if n.Attributes() != nil {
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
