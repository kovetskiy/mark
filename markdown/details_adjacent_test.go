package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdjacentDetailsDoNotNest covers two sections written one after the other
// with nothing between them.
//
// The fragment joining them closes one element and opens the next, which nets
// to zero -- and a decision made on the net change reads that as a
// self-contained tree and hands it to html.Parse, which drops the closer it has
// nothing to match. The sections telescope: the second is published empty,
// inside the first, and the content that belonged to it lands in the first as
// well.
//
// What makes it worth a test of its own is that the result is well-formed, so
// nothing downstream objects: the page publishes, wrong, in silence.
func TestAdjacentDetailsDoNotNest(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	// Distinctive words, because a letter of their own would match inside the
	// markup: "rich-text-body" contains a "d".
	document := "<details><summary>title a</summary>\n\nFIRSTBODY\n\n" +
		"</details><details><summary>title c</summary>\n\nSECONDBODY\n\n</details>\n"

	out, _, err := CompileMarkdown([]byte(document), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Equal(t, 2, strings.Count(out, `ac:name="expand"`), "both sections should be published")

	// Siblings, not one inside the other: the first section's body ends before
	// the second one starts.
	firstBody := strings.Index(out, "<ac:rich-text-body>")
	firstEnd := strings.Index(out, "</ac:rich-text-body>")
	secondStart := strings.Index(out[firstEnd:], `ac:name="expand"`)

	require.Positive(t, firstBody)
	require.Positive(t, firstEnd)
	require.Positive(t, secondStart, "the second section must begin after the first one closes")

	// And each carries its own content.
	assert.Contains(t, out[firstBody:firstEnd], "FIRSTBODY", "the first section keeps its own body")
	assert.NotContains(t, out[firstBody:firstEnd], "SECONDBODY", "and not the second one's")

	require.NoError(t, CheckWellFormed(out))
}

// TestAdjacentDetailsOnOneLineStillPublishBoth is the same shape without the
// blank lines, where the fragments are whole rather than split.
func TestAdjacentDetailsOnOneLineStillPublishBoth(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	document := "<details><summary>a</summary>\nb\n</details>\n" +
		"<details><summary>c</summary>\nd\n</details>\n"

	out, _, err := CompileMarkdown([]byte(document), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	require.NoError(t, CheckWellFormed(out))
}
