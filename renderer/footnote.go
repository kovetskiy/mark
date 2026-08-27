package renderer

import (
	"strconv"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/ast"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// ConfluenceFootnoteRenderer renders Markdown footnotes as something Confluence
// can actually navigate.
//
// Goldmark's own footnote output is ordinary HTML: an id on the marker, an id
// on the note, and href="#id" between them. Confluence keeps none of that. It
// strips the ids out of the storage format and derives its own from element
// text, so both ends of every jump land nowhere -- the page renders, the
// superscript is clickable, and clicking it does nothing.
//
// The replacement is built from the anchor macro, which is bundled with both
// Data Center and Cloud and so needs nothing installed, plus an ac:link naming
// that anchor. Anchors are placed at both ends, which is what makes the way
// back from a note to the sentence that cited it work as well as the way down.
type ConfluenceFootnoteRenderer struct {
	Stdlib *stdlib.Lib
}

// NewConfluenceFootnoteRenderer creates a new instance of the ConfluenceFootnoteRenderer.
func NewConfluenceFootnoteRenderer(stdlib *stdlib.Lib) renderer.NodeRenderer {
	return &ConfluenceFootnoteRenderer{
		Stdlib: stdlib,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceFootnoteRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ext_ast.KindFootnoteLink, r.renderFootnoteLink)
	reg.Register(ext_ast.KindFootnoteBacklink, r.renderFootnoteBacklink)
	reg.Register(ext_ast.KindFootnote, r.renderFootnote)
	reg.Register(ext_ast.KindFootnoteList, r.renderFootnoteList)
}

// footnoteAnchor names the anchor sitting with the note itself.
//
// Anchor names share one namespace with every heading on the page, hence the
// prefix. They are also part of the URL a reader copies out of the address bar,
// so they are spelled out rather than shortened.
func footnoteAnchor(index int) string {
	return "footnote-" + strconv.Itoa(index)
}

// footnoteRefAnchor names the anchor sitting with a marker in the text.
//
// The same note may be cited several times; refIndex distinguishes those, and
// is left off the first so that the common case of a note cited once reads as
// "footnote-ref-3".
func footnoteRefAnchor(index, refIndex int) string {
	name := "footnote-ref-" + strconv.Itoa(index)
	if refIndex > 0 {
		name += "-" + strconv.Itoa(refIndex)
	}
	return name
}

// renderFootnoteLink renders the marker in the running text: an anchor for the
// note to come back to, then a superscript link down to it.
func (r *ConfluenceFootnoteRenderer) renderFootnoteLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*ext_ast.FootnoteLink)

	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:footnote:anchor", struct {
		Anchor string
	}{
		Anchor: footnoteRefAnchor(n.Index, n.RefIndex),
	})
	if err != nil {
		return ast.WalkStop, err
	}

	err = r.Stdlib.Templates.ExecuteTemplate(w, "ac:footnote:ref", struct {
		Anchor string
		Number int
	}{
		Anchor: footnoteAnchor(n.Index),
		Number: n.Index,
	})
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}

// renderFootnoteBacklink renders the arrow at the end of a note that leads back
// to the text that cited it.
func (r *ConfluenceFootnoteRenderer) renderFootnoteBacklink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*ext_ast.FootnoteBacklink)

	// One arrow per citation, numbered only when there is more than one to
	// choose between.
	number := 0
	if n.RefCount > 1 {
		number = n.RefIndex + 1
	}

	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:footnote:backref", struct {
		Anchor string
		Number int
	}{
		Anchor: footnoteRefAnchor(n.Index, n.RefIndex),
		Number: number,
	})
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}

// renderFootnote renders one note as a list item carrying its own anchor.
//
// The item takes no number of its own: goldmark orders the list by the index it
// handed each marker, so the numbering <ol> produces is already the numbering
// the markers show.
func (r *ConfluenceFootnoteRenderer) renderFootnote(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ext_ast.Footnote)

	if !entering {
		_, _ = w.WriteString("</li>\n")
		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString("<li>")

	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:footnote:anchor", struct {
		Anchor string
	}{
		Anchor: footnoteAnchor(n.Index),
	})
	if err != nil {
		return ast.WalkStop, err
	}

	_ = w.WriteByte('\n')

	return ast.WalkContinue, nil
}

// renderFootnoteList renders the notes at the foot of the page.
//
// Goldmark's wrapping <div class="footnotes"> is dropped: Confluence keeps
// neither the element's class nor its ARIA role, so it would arrive as an
// anonymous div and buy nothing. The rule above the list is what actually
// survives to mark the section off.
func (r *ConfluenceFootnoteRenderer) renderFootnoteList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<hr />\n<ol>\n")
	} else {
		_, _ = w.WriteString("</ol>\n")
	}
	return ast.WalkContinue, nil
}
