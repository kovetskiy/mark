package renderer

import (
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// ConfluenceTextRenderer slightly alters the default goldmark behavior for
// inline text block. It allows for soft breaks
// (c.f. https://spec.commonmark.org/0.30/#softbreak)
// to be rendered into HTML as either '\n' (the goldmark default)
// or as ' '.
// This latter option is useful for Confluence,
// which inserts <br> tags into uploaded HTML where it sees '\n'.
// See also https://sembr.org/ for partial motivation.
type ConfluenceTextRenderer struct {
	html.Config
	softBreak rune
}

// NewConfluenceTextRenderer creates a new instance of the ConfluenceTextRenderer
func NewConfluenceTextRenderer(stripNL bool, opts ...html.Option) renderer.NodeRenderer {
	sb := '\n'
	if stripNL {
		sb = ' '
	}
	return &ConfluenceTextRenderer{
		Config:    html.NewConfig(),
		softBreak: sb,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceTextRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindText, r.renderText)
}

// This is taken from https://github.com/yuin/goldmark/blob/v1.5.6/renderer/html/html.go#L648
// with the hardcoded '\n' for soft breaks swapped for the configurable r.softBreak
func (r *ConfluenceTextRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	segment := n.Segment
	if n.IsRaw() {
		r.Writer.RawWrite(w, segment.Value(source))
	} else {
		value := segment.Value(source)
		r.Writer.Write(w, value)
		if n.HardLineBreak() || (n.SoftLineBreak() && r.HardWraps) {
			if r.XHTML {
				_, _ = w.WriteString("<br />\n")
			} else {
				_, _ = w.WriteString("<br>\n")
			}
		} else if n.SoftLineBreak() {
			if r.EastAsianLineBreaks && len(value) != 0 {
				sibling := node.NextSibling()
				if sibling != nil && sibling.Kind() == ast.KindText {
					if siblingText := sibling.(*ast.Text).Text(source); len(siblingText) != 0 {
						thisLastRune := util.ToRune(value, len(value)-1)
						siblingFirstRune, _ := utf8.DecodeRune(siblingText)
						if !(util.IsEastAsianWideRune(thisLastRune) &&
							util.IsEastAsianWideRune(siblingFirstRune)) {
							_ = w.WriteByte(byte(r.softBreak))
						}
					}
				}
			} else {
				_ = w.WriteByte(byte(r.softBreak))
			}
		}
	}
	return ast.WalkContinue, nil
}
