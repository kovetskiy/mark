package transformer

import (
	"bytes"
	"sync"

	"github.com/yuin/goldmark/ast"
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
				strNode.SetRaw(true)
			}
			parent.InsertBefore(parent, item.node, strNode)
			parent.RemoveChild(parent, item.node)
		}
	}
}
