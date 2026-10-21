package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// TestRawContentOutsideTheSourceIsNone: a node that points past the source it
// is read against -- offsets into a sub-document's bytes, say -- gives no bytes
// rather than panicking the whole compile.
func TestRawContentOutsideTheSourceIsNone(t *testing.T) {
	source := []byte("<b>x</b>")
	outside := text.NewIndex(4, 40)

	raw := ast.NewRawHTML(text.NewMultiLineValueFromIndex(outside, nil))

	block := ast.NewHTMLBlock(ast.HTMLBlockKind6)
	block.Value.AppendSegment(text.NewSegment(4, 40))

	textNode := ast.NewText(text.NewSingleLineValueFromIndex(outside, nil))

	for _, node := range []ast.Node{raw, block, textNode} {
		assert.NotPanics(t, func() {
			assert.Nil(t, ExtractNodeRawContent(node, source), node.Kind().String())
			assert.Nil(t, HTMLBytes(node, source), node.Kind().String())
		})
	}

	inside := ast.NewRawHTML(text.NewMultiLineValueFromIndex(text.NewIndex(0, 3), nil))
	assert.Equal(t, "<b>", string(HTMLBytes(inside, source)))
}

// TestRawContentAppendLeavesTheSource: the bytes handed out may be the
// source's own, so an append onto them must copy rather than write over what
// follows in the document.
func TestRawContentAppendLeavesTheSource(t *testing.T) {
	const doc = "<div>\nx\n</div>\n\nafter <b>y</b> z\n"

	source := []byte(doc)
	root := parser.New().Parse(source)

	var nodes []ast.Node
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *ast.HTMLBlock, *ast.RawHTML, *ast.Text:
			if entering {
				nodes = append(nodes, n)
			}
		}

		return ast.WalkContinue, nil
	})
	require.NotEmpty(t, nodes)

	for _, node := range nodes {
		for name, read := range map[string]func(ast.Node, []byte) []byte{
			"HTMLBytes":             HTMLBytes,
			"ExtractNodeRawContent": ExtractNodeRawContent,
		} {
			b := read(node, source)
			if b == nil {
				continue
			}

			_ = append(b, '!')
			assert.Equal(t, doc, string(source), "%s(%s)", name, node.Kind())
		}
	}
}
