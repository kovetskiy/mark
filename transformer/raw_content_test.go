package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
)

// TestRawContentOutsideTheSourceIsNone: a node that points past the source it
// is read against -- offsets into a sub-document's bytes, say -- gives no bytes
// rather than panicking the whole compile, as v1's checks had it.
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
