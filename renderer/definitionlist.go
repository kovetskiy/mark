package renderer

import (
	"github.com/yuin/goldmark/ast"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// ConfluenceDefinitionListRenderer publishes a definition list as a two-column
// table.
//
// The definition list extension is enabled, so "Term\n: Definition" parses --
// and nothing rendered it, so goldmark's own renderer emitted <dl>, <dt> and
// <dd> at the end of the chain. Confluence storage format has no equivalent for
// any of the three: its sanitiser drops the elements and the list arrives as an
// unstructured run of text, with the terms no longer distinguishable from what
// defines them.
//
// A two-column table is the mapping the Confluence editor itself produces for
// this shape, and it is what the elements mean: the term is a heading for its
// row, the definition is the row's content.
type ConfluenceDefinitionListRenderer struct {
	html.Config

	// open holds one frame per definition list currently being written, and
	// only the innermost is ever written to.
	//
	// A definition may itself contain a definition list, and the two lists'
	// rows have nothing to do with each other: with a single pair of flags the
	// inner list reset them on the way in and closed the outer list's row on
	// the way out, leaving a <td> and a <tr> that nothing ever closed. One
	// nested list anywhere in a document made the whole page fail the
	// well-formedness check and never reach Confluence.
	open []listRow
}

// listRow tracks how much of the current row has been written. A row spans
// several nodes -- one term, then any number of descriptions -- so the closing
// tags belong to whatever node comes next rather than to the node that opened
// them.
type listRow struct {
	inRow  bool
	inCell bool
}

func NewConfluenceDefinitionListRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceDefinitionListRenderer{
		Config: html.NewConfig(),
	}
}

func (r *ConfluenceDefinitionListRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ext_ast.KindDefinitionList, r.renderList)
	reg.Register(ext_ast.KindDefinitionTerm, r.renderTerm)
	reg.Register(ext_ast.KindDefinitionDescription, r.renderDescription)
}

func (r *ConfluenceDefinitionListRenderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.open = append(r.open, listRow{})

		_, _ = w.WriteString("<table>\n<tbody>\n")

		return ast.WalkContinue, nil
	}

	r.closeRow(w)
	if len(r.open) > 0 {
		r.open = r.open[:len(r.open)-1]
	}
	_, _ = w.WriteString("</tbody>\n</table>\n")

	return ast.WalkContinue, nil
}

// current returns the list being written, or nil outside one.
func (r *ConfluenceDefinitionListRenderer) current() *listRow {
	if len(r.open) == 0 {
		return nil
	}

	return &r.open[len(r.open)-1]
}

func (r *ConfluenceDefinitionListRenderer) renderTerm(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		// A term begins a row, so whatever row is open ends here.
		r.closeRow(w)

		_, _ = w.WriteString("<tr>\n<th>")
		if row := r.current(); row != nil {
			row.inRow = true
		}

		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString("</th>\n")

	return ast.WalkContinue, nil
}

func (r *ConfluenceDefinitionListRenderer) renderDescription(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	row := r.current()
	if row == nil {
		return ast.WalkContinue, nil
	}

	// A definition written before any term -- which the syntax allows -- still
	// needs a row to sit in.
	if !row.inRow {
		_, _ = w.WriteString("<tr>\n<th></th>\n")
		row.inRow = true
	}

	// Several definitions of one term share its cell rather than each claiming
	// a row of their own, which would leave rows with no term to head them.
	if !row.inCell {
		_, _ = w.WriteString("<td>")
		row.inCell = true

		return ast.WalkContinue, nil
	}

	// A second definition needs something between it and the one before. A
	// loose description brings its own paragraph; a tight one is bare text, and
	// without a break the two definitions run into each other as one word.
	if description, ok := node.(*ext_ast.DefinitionDescription); ok && description.IsTight {
		_, _ = w.WriteString("<br/>")
	}

	return ast.WalkContinue, nil
}

// closeRow finishes whatever row is open, giving a term with no definition an
// empty cell so that every row has both columns.
func (r *ConfluenceDefinitionListRenderer) closeRow(w util.BufWriter) {
	row := r.current()
	if row == nil || !row.inRow {
		return
	}

	if row.inCell {
		_, _ = w.WriteString("</td>\n")
		row.inCell = false
	} else {
		_, _ = w.WriteString("<td></td>\n")
	}

	_, _ = w.WriteString("</tr>\n")
	row.inRow = false
}
