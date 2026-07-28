package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LayoutTransformer lacked the inline-code guard that DetailsTransformer has, so
// documenting a layout directive erased the text: the replacement Text node
// carries its content in an attribute, and goldmark's code-span renderer reads
// only the (empty) segment. LayoutTransformer is registered unconditionally, so
// this affected every document.
func TestLayoutCommentInInlineCodeIsLiteral(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		[]byte("Write `<!-- ac:layout -->` to open a layout.\n"),
		std, "test.md", types.MarkConfig{},
	)
	require.NoError(t, err)

	assert.Contains(t, out, "<code>&lt;!-- ac:layout --&gt;</code>",
		"the directive must survive as literal code text")
	assert.NotContains(t, out, "<code></code>", "the code span must not be emptied")
	assert.NotContains(t, out, "<ac:layout>", "a quoted directive must not open a layout")
}

// A real directive outside a code span must still be converted.
func TestLayoutCommentOutsideCodeSpanStillConverts(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		[]byte("<!-- ac:layout -->\n\nBody.\n\n<!-- ac:layout end -->\n"),
		std, "test.md", types.MarkConfig{},
	)
	require.NoError(t, err)

	assert.Contains(t, out, "<ac:layout>")
	assert.Contains(t, out, "</ac:layout>")
}
