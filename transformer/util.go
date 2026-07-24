package transformer

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
)

func extractNodeRawContent(node ast.Node, source []byte) []byte {
	switch t := node.(type) {
	case *ast.HTMLBlock:
		return t.Text(source)
	case *ast.RawHTML:
		var buf bytes.Buffer
		for i := 0; i < t.Segments.Len(); i++ {
			seg := t.Segments.At(i)
			buf.Write(seg.Value(source))
		}
		return buf.Bytes()
	case *ast.Text:
		return t.Segment.Value(source)
	}
	return nil
}

func convertSegmentsToStrings(doc ast.Node, source []byte) {
	var nodesToReplace []struct {
		textNode *ast.Text
		val      []byte
	}

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if textNode, ok := n.(*ast.Text); ok {
			val := textNode.Segment.Value(source)
			valCopy := make([]byte, len(val))
			copy(valCopy, val)
			nodesToReplace = append(nodesToReplace, struct {
				textNode *ast.Text
				val      []byte
			}{textNode: textNode, val: valCopy})
		}
		return ast.WalkContinue, nil
	})

	for _, item := range nodesToReplace {
		parent := item.textNode.Parent()
		if parent != nil {
			strNode := ast.NewString(item.val)
			parent.InsertBefore(parent, item.textNode, strNode)
			parent.RemoveChild(parent, item.textNode)
		}
	}
}
