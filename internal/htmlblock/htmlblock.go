// Package htmlblock reads an HTML block's lines the way goldmark v1 handed
// them out, for the code here that was written against that.
package htmlblock

import (
	"bytes"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
)

// Lines returns the lines of an HTML block without the line that closed it.
//
// goldmark v1 kept the closing line of a block of kinds 1 to 5 -- the line
// holding "</script>", "-->", "?>", ">" or "]]>" -- apart from the others, in
// ClosureLine, and Lines() left it out. goldmark v2 keeps it among the others.
// A reader that looks at a block line by line, such as the blockquote
// classifier and the header scan, saw only the lines before it under v1.
//
// The closing line is never the first one: a block whose first line closes it
// is that one line, and v1 kept it in Lines(). Nor can a line before the last
// close the block, since it would have ended there; so the last line closed it
// exactly when it holds the block's closing marker. A block that ran out
// before closing, at the end of the document or of its container, keeps every
// line.
func Lines(n *ast.HTMLBlock, source []byte) []text.Segment {
	lines := n.Value.Segments()
	if len(lines) < 2 {
		return lines
	}

	if closes(n.HTMLBlockKind, lines[len(lines)-1].Bytes(source)) {
		return lines[:len(lines)-1]
	}

	return lines
}

// closes reports whether line holds the marker that ends an HTML block of the
// given kind, as goldmark's parser tests for it.
func closes(kind ast.HTMLBlockKind, line []byte) bool {
	switch kind {
	case ast.HTMLBlockKind1:
		lower := bytes.ToLower(line)
		for _, tag := range []string{"</script>", "</pre>", "</style>", "</textarea>"} {
			if bytes.Contains(lower, []byte(tag)) {
				return true
			}
		}

		return false
	case ast.HTMLBlockKind2:
		return bytes.Contains(line, []byte("-->"))
	case ast.HTMLBlockKind3:
		return bytes.Contains(line, []byte("?>"))
	case ast.HTMLBlockKind4:
		return bytes.Contains(line, []byte(">"))
	case ast.HTMLBlockKind5:
		return bytes.Contains(line, []byte("]]>"))
	}

	return false
}
