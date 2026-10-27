package transformer

import (
	"bytes"
	"slices"

	"github.com/yuin/goldmark/v2/ast"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// The footnote renderer reads the notes gathered into one FootnoteList at the
// end of the document, with a backlink per citation, where goldmark leaves
// each definition where it was written. The two transformers below build the
// list.

// FootnoteList holds the footnote definitions of a document.
type FootnoteList struct {
	ast.BaseBlock

	// Indices is the number each definition is cited by, keyed by the
	// definition. A definition nothing cites has none.
	Indices map[ast.Node]int
}

// KindFootnoteList is the ast.NodeKind of a FootnoteList.
var KindFootnoteList = ast.NewNodeKind("MarkFootnoteList")

// Kind implements ast.Node.
func (n *FootnoteList) Kind() ast.NodeKind {
	return KindFootnoteList
}

// Dump implements ast.Node.
func (n *FootnoteList) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, nil)
}

// NewFootnoteList returns an empty FootnoteList.
func NewFootnoteList() *FootnoteList {
	n := &FootnoteList{Indices: map[ast.Node]int{}}
	n.Init(n)

	return n
}

// FootnoteIndex is the number a footnote definition is cited by, or -1 for a
// definition nothing cites or one outside a FootnoteList.
func FootnoteIndex(definition ast.Node) int {
	list, ok := definition.Parent().(*FootnoteList)
	if !ok {
		return -1
	}

	if index, ok := list.Indices[definition]; ok {
		return index
	}

	return -1
}

// FootnoteBacklink leads from the end of a note back to one citation of it.
type FootnoteBacklink struct {
	ast.BaseInline

	// Index is the number of the note.
	Index int

	// RefCount is how many times the note is cited, and RefIndex which of
	// those citations this leads back to, counting from 0.
	RefCount int
	RefIndex int
}

// KindFootnoteBacklink is the ast.NodeKind of a FootnoteBacklink.
var KindFootnoteBacklink = ast.NewNodeKind("MarkFootnoteBacklink")

// Kind implements ast.Node.
func (n *FootnoteBacklink) Kind() ast.NodeKind {
	return KindFootnoteBacklink
}

// Dump implements ast.Node.
func (n *FootnoteBacklink) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"Index":    n.Index,
		"RefCount": n.RefCount,
		"RefIndex": n.RefIndex,
	})
}

// NewFootnoteBacklink returns a FootnoteBacklink.
func NewFootnoteBacklink(index, refCount, refIndex int) *FootnoteBacklink {
	n := &FootnoteBacklink{Index: index, RefCount: refCount, RefIndex: refIndex}
	n.Init(n)

	return n
}

// FootnoteListTransformer gathers the footnote definitions into a
// FootnoteList standing where the first of them was written. It runs before
// every other transformer, so that they all see the list.
type FootnoteListTransformer struct{}

// NewFootnoteListTransformer returns a FootnoteListTransformer.
func NewFootnoteListTransformer() *FootnoteListTransformer {
	return &FootnoteListTransformer{}
}

// Transform implements parser.ASTTransformer.
func (t *FootnoteListTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	if !mayHaveFootnotes(source) {
		return
	}

	var definitions []*extast.FootnoteDefinition

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if definition, ok := node.(*extast.FootnoteDefinition); ok && entering {
			definitions = append(definitions, definition)
		}

		return ast.WalkContinue, nil
	})

	if len(definitions) == 0 {
		return
	}

	list := NewFootnoteList()
	definitions[0].Parent().InsertBefore(definitions[0], list)

	var lists []*ast.List
	for _, definition := range definitions {
		for ancestor := definition.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
			if l, ok := ancestor.(*ast.List); ok && !slices.Contains(lists, l) {
				lists = append(lists, l)
			}
		}

		definition.Parent().RemoveChild(definition)
		list.AppendChild(definition)
	}

	for _, l := range lists {
		l.IsTight = isTightList(l)
	}

	indices := renumberFootnoteReferences(doc, source)

	seen := map[string]bool{}
	for _, definition := range definitions {
		// A label defined twice is cited by its first definition only.
		label := definition.Label.Str(source)
		if index, ok := indices[label]; ok && !seen[label] {
			list.Indices[definition] = index
		}
		seen[label] = true
	}
}

// isTightList decides whether list is tight by the rule goldmark's list parser
// applies when it closes the list: loose if an item but the first follows a
// blank line, or a block of an item but its first does. goldmark decides with
// the footnote definitions still in the list, so a definition after a blank
// line makes it loose; asked again once they are gone, the rule decides by the
// list's own content. The FootnoteList left where the first definition stood
// counts, but it never follows a blank line.
func isTightList(list *ast.List) bool {
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if item != list.FirstChild() && hasBlankPreviousLines(item) {
			return false
		}

		if item.FirstChild() == nil {
			continue
		}

		for block := item.FirstChild().NextSibling(); block != nil; block = block.NextSibling() {
			if hasBlankPreviousLines(block) {
				return false
			}
		}
	}

	return true
}

// hasBlankPreviousLines reports whether node is a block that follows a blank
// line.
func hasBlankPreviousLines(node ast.Node) bool {
	block, ok := node.(ast.BlockNode)

	return ok && block.HasBlankPreviousLines()
}

// renumberFootnoteReferences numbers the citations in document order, with
// the FootnoteList where it stands, and returns the number each cited label
// got: the citations inside the notes count from where the list stands -- the
// first definition -- and not from where each note was written. goldmark
// numbers them in place, so a definition written ahead of some citations
// would renumber the notes. Which citation of a note each one is follows the
// same order.
func renumberFootnoteReferences(doc ast.Node, source []byte) map[string]int {
	indices := map[string]int{}
	refCounts := map[string]int{}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		reference, ok := node.(*extast.FootnoteReference)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}

		label := reference.Label.Str(source)
		index, seen := indices[label]
		if !seen {
			index = len(indices) + 1
			indices[label] = index
		}

		reference.Index = index
		reference.RefIndex = refCounts[label]
		refCounts[label]++

		return ast.WalkContinue, nil
	})

	return indices
}

// FootnoteBacklinkTransformer moves the FootnoteList to the end of the
// document, drops the notes nothing cites, numbers the rest in order and gives
// each a backlink per citation. It runs after every other transformer, so
// that it numbers the citations they leave.
type FootnoteBacklinkTransformer struct{}

// NewFootnoteBacklinkTransformer returns a FootnoteBacklinkTransformer.
func NewFootnoteBacklinkTransformer() *FootnoteBacklinkTransformer {
	return &FootnoteBacklinkTransformer{}
}

// Transform implements parser.ASTTransformer.
func (t *FootnoteBacklinkTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	if !mayHaveFootnotes(reader.Source()) {
		return
	}

	var list *FootnoteList
	counter := map[int]int{}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *FootnoteList:
			if list == nil {
				list = n
			}
		case *extast.FootnoteReference:
			counter[n.Index]++
		}

		return ast.WalkContinue, nil
	})

	if list == nil {
		return
	}

	var notes []ast.Node
	for note := list.FirstChild(); note != nil; {
		next := note.NextSibling()

		index, cited := list.Indices[note]
		if !cited {
			list.RemoveChild(note)
			note = next

			continue
		}

		var container = note
		if last := note.LastChild(); last != nil && ast.IsParagraph(last) {
			container = last
		}

		for i := 0; i < max(counter[index], 1); i++ {
			container.AppendChild(NewFootnoteBacklink(index, counter[index], i))
		}

		notes = append(notes, note)
		note = next
	}

	if parent := list.Parent(); parent != nil {
		parent.RemoveChild(list)
	}

	if len(notes) == 0 {
		return
	}

	slices.SortStableFunc(notes, func(a, b ast.Node) int {
		return list.Indices[a] - list.Indices[b]
	})

	for _, note := range notes {
		list.AppendChild(note)
	}

	doc.AppendChild(list)
}

// mayHaveFootnotes reports whether source could hold a footnote at all, which
// both its reference and its definition open with "[^". Most pages have none,
// and the footnote transformers need not walk the tree of one.
func mayHaveFootnotes(source []byte) bool {
	return bytes.Contains(source, []byte("[^"))
}
