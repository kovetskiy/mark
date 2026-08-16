package transformer

import (
	"bytes"
	"sync"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func getBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	if buf != nil {
		buf.Reset()
		bufferPool.Put(buf)
	}
}

func getNodeLineNumber(node ast.Node, source []byte) int {
	offset := -1
	switch t := node.(type) {
	case *ast.HTMLBlock:
		if t.Lines().Len() > 0 {
			offset = t.Lines().At(0).Start
		}
	case *ast.Text:
		offset = t.Segment.Start
	case *ast.RawHTML:
		if t.Segments.Len() > 0 {
			offset = t.Segments.At(0).Start
		}
	}
	if offset < 0 || offset >= len(source) {
		return 1
	}
	return bytes.Count(source[:offset], []byte("\n")) + 1
}

func extractHTMLBlockBytes(t *ast.HTMLBlock, source []byte) []byte {
	lines := t.Lines()
	if lines.Len() == 1 && !t.HasClosure() {
		seg := lines.At(0)
		if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
			return seg.Value(source)
		}
		return nil
	}

	buf := getBuffer()
	defer putBuffer(buf)

	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
			buf.Write(seg.Value(source))
		}
	}
	if t.HasClosure() && t.ClosureLine.Start >= 0 && t.ClosureLine.Stop <= len(source) {
		buf.Write(t.ClosureLine.Value(source))
	}

	res := make([]byte, buf.Len())
	copy(res, buf.Bytes())
	return res
}

func ExtractNodeRawContent(node ast.Node, source []byte) []byte {
	switch t := node.(type) {
	case *ast.HTMLBlock:
		return extractHTMLBlockBytes(t, source)
	case *ast.RawHTML:
		if t.Segments.Len() == 1 {
			seg := t.Segments.At(0)
			if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
				return seg.Value(source)
			}
			return nil
		}
		buf := getBuffer()
		defer putBuffer(buf)
		for i := 0; i < t.Segments.Len(); i++ {
			seg := t.Segments.At(i)
			if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
				buf.Write(seg.Value(source))
			}
		}
		res := make([]byte, buf.Len())
		copy(res, buf.Bytes())
		return res
	case *ast.Text:
		if t.Segment.Start >= 0 && t.Segment.Stop <= len(source) && t.Segment.Start <= t.Segment.Stop {
			return t.Segment.Value(source)
		}
		return nil
	case *ast.String:
		return t.Value
	default:
		if node.HasChildren() {
			buf := getBuffer()
			defer putBuffer(buf)
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				buf.Write(ExtractNodeRawContent(child, source))
			}
			res := make([]byte, buf.Len())
			copy(res, buf.Bytes())
			return res
		}
	}
	return nil
}

func convertSegmentsToStrings(doc ast.Node, source []byte) {
	type replaceItem struct {
		node  ast.Node
		val   []byte
		isRaw bool
	}
	var nodesToReplace []replaceItem

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Text:
			if t.Segment.Start >= 0 && t.Segment.Stop <= len(source) && t.Segment.Start <= t.Segment.Stop {
				val := t.Segment.Value(source)
				valCopy := make([]byte, len(val))
				copy(valCopy, val)
				nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: valCopy, isRaw: false})
			}
		case *ast.HTMLBlock:
			val := extractHTMLBlockBytes(t, source)
			valCopy := make([]byte, len(val))
			copy(valCopy, val)
			nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: valCopy, isRaw: true})
		case *ast.RawHTML:
			if t.Segments.Len() == 1 {
				seg := t.Segments.At(0)
				if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
					val := seg.Value(source)
					valCopy := make([]byte, len(val))
					copy(valCopy, val)
					nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: valCopy, isRaw: true})
				}
			} else {
				buf := getBuffer()
				for i := 0; i < t.Segments.Len(); i++ {
					seg := t.Segments.At(i)
					if seg.Start >= 0 && seg.Stop <= len(source) && seg.Start <= seg.Stop {
						buf.Write(seg.Value(source))
					}
				}
				valCopy := make([]byte, buf.Len())
				copy(valCopy, buf.Bytes())
				putBuffer(buf)
				nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: valCopy, isRaw: true})
			}
		}
		return ast.WalkContinue, nil
	})

	for _, item := range nodesToReplace {
		parent := item.node.Parent()
		if parent != nil {
			strNode := ast.NewString(item.val)
			if item.isRaw {
				// Both flags, and the second is the one that matters. goldmark's
				// "raw" only means backslash escapes are not processed -- its
				// RawWrite still runs every byte through EscapeHTMLByte, so a
				// string marked raw alone comes out as &lt;ac:structured-macro.
				// IsCode is what writes the value verbatim, which is what
				// Confluence markup from a macro or an include has to be.
				strNode.SetRaw(true)
				strNode.SetCode(true)
			}
			parent.InsertBefore(parent, item.node, strNode)
			parent.RemoveChild(parent, item.node)
		}
	}
}

// newSubParser builds the parser used for content that arrives by expanding a
// macro or an include.
//
// It carries the block extensions the document itself is parsed with, because
// the expansion is ordinary Markdown and an author expects it to behave that
// way: a macro whose body is a table should produce a table, not a paragraph of
// pipes. The mark extension is deliberately not among them -- it is what is
// running right now, and the pipeline already re-runs until the tree settles,
// so nesting a second copy here would only find the same work twice.
func newSubParser() parser.Parser {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.Footnote,
			extension.DefinitionList,
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
			),
			extension.GFM,
		),
		goldmark.WithParserOptions(
			parser.WithInlineParsers(
				// Above goldmark's link parser, so <ac:.../> tags are kept whole
				// rather than picked apart.
				util.Prioritized(cparser.NewConfluenceTagParser(), 99),
			),
		),
	).Parser()
}

// insideCode reports whether a node sits within a code span.
//
// Text in a code span is a sample, not something to substitute into. Fenced and
// indented blocks need no check: their content is a CodeBlock, which is not a
// node kind macro substitution looks at in the first place.
func insideCode(node ast.Node) bool {
	for n := node; n != nil; n = n.Parent() {
		if n.Kind() == ast.KindCodeSpan {
			return true
		}
	}

	return false
}

// expandedAttr marks a node as something a macro produced.
//
// The pipeline runs until the tree stops changing, so a macro's output is
// offered back to it on the next pass. That matters because macro output
// routinely contains the text that matched it -- the jira macro turns
// MYJIRA-123 into a tag with MYJIRA-123 inside -- and without a marker the
// second pass expands the expansion, nesting one macro inside another.
//
// Content brought in by an *include* is deliberately not marked. A document
// that includes a fragment expects macros to apply to it, and that is the one
// thing separating the two cases: an include yields text to work on, a macro
// yields its own finished output.
var expandedAttr = []byte("mark:macro-expanded")

func markExpanded(node ast.Node) {
	node.SetAttribute(expandedAttr, true)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		markExpanded(child)
	}
}

// isExpansion reports whether a node, or anything it sits inside, is macro
// output.
func isExpansion(node ast.Node) bool {
	for n := node; n != nil; n = n.Parent() {
		if _, ok := n.Attribute(expandedAttr); ok {
			return true
		}
	}

	return false
}

// spliceExpansion puts a macro's expansion where the text that matched it was.
//
// What the expansion replaces depends on its shape. One that parsed to a single
// paragraph is inline -- an issue key becoming a status lozenge -- and its
// inline children take the place of the matched text, leaving the surrounding
// sentence intact. Anything else is a block: a macro wrapping a list or a
// table cannot live inside the paragraph that named it, so it replaces that
// paragraph outright.
//
// The block case is only taken when the matched text was the paragraph's whole
// content. A paragraph holding a block macro and a sentence beside it has no
// correct reading, and dropping the sentence to place the block would lose
// what the author wrote.
func spliceExpansion(parent ast.Node, matched ast.Node, subDoc ast.Node, lead, trail []byte) {
	first := subDoc.FirstChild()
	inline := first != nil && first == subDoc.LastChild() && first.Kind() == ast.KindParagraph

	if !inline && parent.Kind() == ast.KindParagraph &&
		parent.FirstChild() == matched && parent.LastChild() == matched &&
		parent.Parent() != nil {
		grandparent := parent.Parent()
		for subDoc.FirstChild() != nil {
			child := subDoc.FirstChild()
			subDoc.RemoveChild(subDoc, child)
			markExpanded(child)
			grandparent.InsertBefore(grandparent, parent, child)
		}
		grandparent.RemoveChild(grandparent, parent)

		return
	}

	source := subDoc
	if inline {
		// Unwrap: the paragraph the sub-parse wrapped its inlines in would
		// otherwise be nested inside the paragraph being edited.
		source = first
	}

	if inline && len(lead) > 0 {
		parent.InsertBefore(parent, matched, ast.NewString(lead))
	}

	for source.FirstChild() != nil {
		child := source.FirstChild()
		source.RemoveChild(source, child)
		markExpanded(child)
		parent.InsertBefore(parent, matched, child)
	}

	if inline && len(trail) > 0 {
		parent.InsertBefore(parent, matched, ast.NewString(trail))
	}

	parent.RemoveChild(parent, matched)
}

// splitEdgeSpace separates the whitespace at each end of an expansion from the
// content between them.
func splitEdgeSpace(data []byte) (lead, trail, body []byte) {
	body = bytes.TrimLeft(data, " \t")
	lead = data[:len(data)-len(body)]

	trimmed := bytes.TrimRight(body, " \t")
	trail = body[len(trimmed):]

	return lead, trail, trimmed
}
