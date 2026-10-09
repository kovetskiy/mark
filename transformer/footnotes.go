package transformer

import (
	"bytes"
	"slices"

	"github.com/yuin/goldmark/v2/ast"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// goldmark v2 leaves each footnote definition where it was written and has its
// own renderer gather them up at the end of the page. goldmark v1 did that
// gathering in the AST: the definitions went into one FootnoteList standing
// where the first of them was written, the list was moved to the end of the
// document once everything else had run, and each note got a backlink node
// per citation. The footnote renderer here was written against that tree, and
// so were the transformers in between, which saw the notes inside the list.
// The two transformers below build it again, at the two points v1 did.

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
// every other transformer, as v1's parser did this while parsing.
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
	indices := map[string]int{}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *extast.FootnoteDefinition:
			definitions = append(definitions, n)
		case *extast.FootnoteReference:
			label := n.Label.Str(source)
			if _, seen := indices[label]; !seen {
				indices[label] = n.Index
			}
		}

		return ast.WalkContinue, nil
	})

	if len(definitions) == 0 {
		return
	}

	list := NewFootnoteList()
	definitions[0].Parent().InsertBefore(definitions[0], list)

	seen := map[string]bool{}
	for _, definition := range definitions {
		definition.Parent().RemoveChild(definition)
		list.AppendChild(definition)

		// A label defined twice is cited by its first definition only.
		label := definition.Label.Str(source)
		if index, ok := indices[label]; ok && !seen[label] {
			list.Indices[definition] = index
		}
		seen[label] = true
	}
}

// FootnoteBacklinkTransformer moves the FootnoteList to the end of the
// document, drops the notes nothing cites, numbers the rest in order and gives
// each a backlink per citation. It runs after every other transformer, at the
// priority v1's own footnote transformer had.
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
