package renderer

import (
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceTextRenderer struct {
	html.Config
	softBreak rune
}

// NewConfluenceTextRenderer creates a new instance of the renderer with GitHub Alerts support
func NewConfluenceTextRenderer(stripNewlines bool, opts ...html.Option) renderer.NodeRenderer {
	sb := '\n'
	if stripNewlines {
		sb = ' '
	}
	return &ConfluenceTextRenderer{
		Config:    html.NewConfig(),
		softBreak: sb,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs
func (r *ConfluenceTextRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindText, r.renderText)
}

// This is taken from https://github.com/yuin/goldmark/blob/v1.6.0/renderer/html/html.go#L719
func (r *ConfluenceTextRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*ast.Text)

	// Check if this text node has replacement content from the GHAlerts transformer
	if replacementContent, hasAttribute := node.Attribute([]byte("replacement-content")); hasAttribute && replacementContent != nil {
		if contentBytes, ok := replacementContent.([]byte); ok {
			_, err := w.Write(contentBytes)
			if err != nil {
				return ast.WalkStop, err
			}
			return ast.WalkContinue, nil
		}
	}

	// Default text rendering behavior (same as original ConfluenceTextRenderer)
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
			if r.EastAsianLineBreaks != html.EastAsianLineBreaksNone && len(value) != 0 {
				sibling := node.NextSibling()
				if sibling != nil && sibling.Kind() == ast.KindText {
					if siblingText := sibling.(*ast.Text).Value(source); len(siblingText) != 0 {
						thisLastRune := util.ToRune(value, len(value)-1)
						siblingFirstRune, _ := utf8.DecodeRune(siblingText)
						// Inline the softLineBreak function as it's not public
						writeLineBreak := false
						switch r.EastAsianLineBreaks {
						case html.EastAsianLineBreaksNone:
							writeLineBreak = false
						case html.EastAsianLineBreaksSimple:
							writeLineBreak = !util.IsEastAsianWideRune(thisLastRune) || !util.IsEastAsianWideRune(siblingFirstRune)
						case html.EastAsianLineBreaksCSS3Draft:
							writeLineBreak = eastAsianLineBreaksCSS3DraftSoftLineBreak(thisLastRune, siblingFirstRune)
						}

						if writeLineBreak {
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

func eastAsianLineBreaksCSS3DraftSoftLineBreak(thisLastRune rune, siblingFirstRune rune) bool {
	// Implements CSS text level3 Segment Break Transformation Rules with some enhancements.
	// References:
	//   - https://www.w3.org/TR/2020/WD-css-text-3-20200429/#line-break-transform
	//   - https://github.com/w3c/csswg-drafts/issues/5086

	// Rule1:
	//   If the character immediately before or immediately after the segment break is
	//   the zero-width space character (U+200B), then the break is removed, leaving behind the zero-width space.
	if thisLastRune == '\u200B' || siblingFirstRune == '\u200B' {
		return false
	}

	// Rule2:
	//   Otherwise, if the East Asian Width property of both the character before and after the segment break is
	//   F, W, or H (not A), and neither side is Hangul, then the segment break is removed.
	thisLastRuneEastAsianWidth := util.EastAsianWidth(thisLastRune)
	siblingFirstRuneEastAsianWidth := util.EastAsianWidth(siblingFirstRune)
	if (thisLastRuneEastAsianWidth == "F" ||
		thisLastRuneEastAsianWidth == "W" ||
		thisLastRuneEastAsianWidth == "H") &&
		(siblingFirstRuneEastAsianWidth == "F" ||
			siblingFirstRuneEastAsianWidth == "W" ||
			siblingFirstRuneEastAsianWidth == "H") {
		return unicode.Is(unicode.Hangul, thisLastRune) || unicode.Is(unicode.Hangul, siblingFirstRune)
	}

	// Rule3:
	//   Otherwise, if either the character before or after the segment break belongs to
	//   the space-discarding character set and it is a Unicode Punctuation (P*) or U+3000,
	//   then the segment break is removed.
	if util.IsSpaceDiscardingUnicodeRune(thisLastRune) ||
		unicode.IsPunct(thisLastRune) ||
		thisLastRune == '\u3000' ||
		util.IsSpaceDiscardingUnicodeRune(siblingFirstRune) ||
		unicode.IsPunct(siblingFirstRune) ||
		siblingFirstRune == '\u3000' {
		return false
	}

	// Rule4:
	//   Otherwise, the segment break is converted to a space (U+0020).
	return true
}
