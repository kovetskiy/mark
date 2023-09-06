package renderer

import (
	"unicode/utf8"

	//"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceTextRenderer struct {
	html.Config
	softBreak rune
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
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
