package renderer

import (
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// ConfluenceTextRenderer slightly alters the default goldmark behaviour for
// inline text. It allows for soft breaks
// (c.f. https://spec.commonmark.org/0.30/#softbreak)
// to be rendered as either '\n' (the goldmark default) or as ' '.
// The latter is useful for Confluence, which inserts <br> tags into uploaded
// HTML where it sees '\n'. See also https://sembr.org/ for partial motivation.
//
// It also writes out the replacement-content a transformer has left on a Text
// node in place of the node's own text. Both compile paths use it.
type ConfluenceTextRenderer struct {
	htmlOptions

	// softBreak is written verbatim with WriteByte and is only ever '\n' or
	// ' ', so it is a byte rather than a rune -- a rune would imply multi-byte
	// values that the write path cannot represent.
	softBreak byte
}

// NewConfluenceTextRenderer creates a new instance of the text renderer
func NewConfluenceTextRenderer(stripNewlines bool, opts ...html.Option) html.Extension {
	sb := byte('\n')
	if stripNewlines {
		sb = ' '
	}
	r := &ConfluenceTextRenderer{
		softBreak: sb,
	}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceTextRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindText: contextNodeRenderer(r.renderText),
	})}
}

// renderText is goldmark's own text renderer with the hardcoded '\n' for soft
// breaks swapped for the configurable r.softBreak, and a check for
// replacement-content before any of it.
//
// replacement-content is how DetailsTransformer and LayoutTransformer hand
// their rewritten markup to the renderer. The legacy compile path used to have
// a copy of this renderer without the check, and since it runs
// DetailsTransformer too, every <details> block on that path was published as
// the empty node the transformer had left in its place.
func (r *ConfluenceTextRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*ast.Text)

	// Check if a transformer has left rewritten markup on this node
	if replacementContent, hasAttribute := node.Attribute(ctransformer.ReplacementContentAttribute); hasAttribute {
		if _, err := replacementContent.WriteTo(w, source); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	// Default text rendering behavior: the value is decoded already, and
	// only has to be escaped.
	_, _ = n.Value.WriteTo(html.ContextTextWriter(rc), source)

	if n.HardLineBreak() || (n.SoftLineBreak() && r.HardWraps) {
		if r.XHTML {
			_, _ = w.WriteString("<br />\n")
		} else {
			_, _ = w.WriteString("<br>\n")
		}
	} else if n.SoftLineBreak() {
		if r.LineBreakStrategy == nil || r.softLineBreak(n, source) {
			_ = w.WriteByte(r.softBreak)
		}
	}

	return ast.WalkContinue, nil
}

// softLineBreak asks the configured line break strategy whether the break
// after n is kept, given the characters either side of it.
func (r *ConfluenceTextRenderer) softLineBreak(n *ast.Text, source []byte) bool {
	last, ok := n.LastRune(source)
	if !ok {
		return true
	}

	sibling, ok := n.NextSibling().(ast.InlineNode)
	if !ok {
		return true
	}

	first, ok := sibling.FirstRune(source)
	if !ok {
		return true
	}

	return r.LineBreakStrategy.SoftLineBreak(last, first)
}
