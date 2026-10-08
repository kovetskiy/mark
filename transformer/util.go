package transformer

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yuin/goldmark/v2/ast"
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
		if lines := t.Value.Segments(); len(lines) > 0 {
			offset = lines[0].Start
		}
	case *ast.Text:
		if !t.Value.IsOwned() {
			offset = t.Value.Index().Start
		}
	case *ast.RawHTML:
		if !t.Value.IsOwned() {
			offset = t.Value.Index().Start
		}
	}
	if offset < 0 || offset >= len(source) {
		return 1
	}
	return bytes.Count(source[:offset], []byte("\n")) + 1
}

// extractHTMLBlockBytes returns a block's bytes, its closing line included:
// goldmark v2 keeps that among the others, where v1 held it apart.
func extractHTMLBlockBytes(t *ast.HTMLBlock, source []byte) []byte {
	return bytes.Clone(t.Value.Bytes(source))
}

// ExtractDirectiveContent is ExtractNodeRawContent with inline code left out.
//
// A directive is only a directive where it would run. The pass that expands
// includes and macros before goldmark ever sees the document skips code
// regions, and metadata.CodeRegions covers spans as well as fenced blocks -- but
// the AST transformers that pick up what that pass left behind had no
// equivalent, and they match a paragraph at a time. So
//
//	To include, write `<!-- Include: nav.md -->` in your doc.
//
// pulled nav.md into the page and dropped the code span with it, and naming a
// file that does not exist failed the whole compile. Writing about mark's own
// syntax broke the document doing it.
//
// Fenced blocks were never affected: they hold no child nodes, so there is
// nothing here to concatenate.
func ExtractDirectiveContent(node ast.Node, source []byte) []byte {
	if node.Kind() == ast.KindCodeSpan {
		return nil
	}

	// The leaf kinds carry their own bytes; only the recursive case has
	// children a code span could be hiding among.
	switch node.(type) {
	case *ast.HTMLBlock, *ast.RawHTML, *ast.Text, *String:
		return ExtractNodeRawContent(node, source)
	}

	if !node.HasChildren() {
		return ExtractNodeRawContent(node, source)
	}

	buf := getBuffer()
	defer putBuffer(buf)

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		buf.Write(ExtractDirectiveContent(child, source))
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())

	return out
}

func ExtractNodeRawContent(node ast.Node, source []byte) []byte {
	switch t := node.(type) {
	case *ast.HTMLBlock:
		return extractHTMLBlockBytes(t, source)
	case *ast.RawHTML:
		return bytes.Clone(t.Value.Bytes(source))
	case *ast.Text:
		return t.Value.Bytes(source)
	case *ast.CodeSpan:
		// A span's text is its value in goldmark v2, where v1 kept it on Text
		// children for the default case below to collect.
		return t.Value.Bytes(source)
	case *String:
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

// errUnmovableNode reports a node from a parsed sub-document that cannot be
// spliced into the document being transformed.
var errUnmovableNode = errors.New("cannot be carried into the document it was expanded into")

// unmovableNode names the first node under doc that renders straight from
// its own source in a way convertSegmentsToStrings does not rebase, or returns
// nil when there is none.
//
// The sub-document is parsed from the expanded bytes and rendered against the
// outer document's: a code block reads its lines, and a fenced one its info
// string too, as offsets into whatever source it is handed, and an autolink its
// URL. Spliced in as they were, a fenced block in an included fragment came out
// with its language and body sliced from the middle of the outer page -- still
// well-formed, so nothing downstream noticed. The code block renderer reads
// from nowhere else, so the caller fails instead.
func unmovableNode(doc ast.Node) ast.Node {
	var found ast.Node

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n.(type) {
		case *ast.CodeBlock, *ast.AutoLink:
			found = n
			return ast.WalkStop, nil
		}

		return ast.WalkContinue, nil
	})

	return found
}

// nodeName names a node's kind as goldmark v1 did, which kept fenced and
// indented code blocks apart.
func nodeName(n ast.Node) string {
	if block, ok := n.(*ast.CodeBlock); ok && block.CodeBlockKind == ast.CodeBlockKindFenced {
		return "FencedCodeBlock"
	}

	return n.Kind().String()
}

// convertSegmentsToStrings detaches a parsed sub-document from the bytes it was
// parsed out of, so that its nodes can be moved into another document. It
// refuses, before changing anything, a sub-document holding a node it cannot
// detach.
//
// Text, raw HTML and HTML blocks become Strings, as goldmark v1's ast.String
// they replace here always did. Everything else that goldmark v2 points into
// the source -- a link's destination and title, a code span, an attribute --
// is given its own copy where it stands, since v1 held those as bytes already.
func convertSegmentsToStrings(doc ast.Node, source []byte) error {
	if n := unmovableNode(doc); n != nil {
		return fmt.Errorf("%s %w", nodeName(n), errUnmovableNode)
	}

	type replaceItem struct {
		node     ast.Node
		val      []byte
		verbatim bool
	}
	var nodesToReplace []replaceItem

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		for _, attribute := range n.Attributes() {
			n.SetAttribute(attribute.Name, ownedMulti(attribute.Value, source))
		}

		switch t := n.(type) {
		case *ast.Text:
			nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: bytes.Clone(t.Value.Bytes(source)), verbatim: false})
		case *ast.HTMLBlock:
			nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: extractHTMLBlockBytes(t, source), verbatim: true})
		case *ast.RawHTML:
			nodesToReplace = append(nodesToReplace, replaceItem{node: t, val: bytes.Clone(t.Value.Bytes(source)), verbatim: true})
		case *ast.CodeSpan:
			if !t.Value.IsOwned() {
				t.Value = PlainValue(strings.Clone(t.Value.Value(source)))
			}
		case *ast.Link:
			t.Destination = ownedSingle(t.Destination, source)
			t.Title = ownedMulti(t.Title, source)
		case *ast.Image:
			t.Destination = ownedSingle(t.Destination, source)
			t.Title = ownedMulti(t.Title, source)
		}
		return ast.WalkContinue, nil
	})

	for _, item := range nodesToReplace {
		parent := item.node.Parent()
		if parent != nil {
			strNode := NewString(item.val)
			if item.verbatim {
				// Code, not escaped text: the <ac:structured-macro> a macro or
				// include exists to carry would otherwise be published as
				// visible literal text.
				strNode.Code = true
			}
			parent.InsertBefore(item.node, strNode)
			parent.RemoveChild(item.node)
		}
	}

	return nil
}
