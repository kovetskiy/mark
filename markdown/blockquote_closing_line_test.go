package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlockQuoteTypeIgnoresAClosingLine pins the admonition a blockquote
// opening with a multi-line HTML block becomes. goldmark v1 kept the line that
// closed the block apart from its other lines, so a marker written there --
// "Warn -->" -- did not classify the quote; goldmark v2 keeps it among them,
// and it did. Every expectation is what the v1 code produced, on both paths.
func TestBlockQuoteTypeIgnoresAClosingLine(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	const warning = `<ac:structured-macro ac:name="warning">`
	const info = `<ac:structured-macro ac:name="info">`

	for _, tc := range []struct {
		name, input, want, not string
	}{
		{"marker on a comment's closing line", "> <!--\n> Warn -->\n> text\n", "<blockquote>", warning},
		{"marker after a comment's first line", "> <!-- x\n> Info -->\n\n> more\n", "<blockquote>", info},
		{"marker on the last of three lines", "> <!-- x\n> y\n> Tip -->\n> text\n", "<blockquote>", "ac:structured-macro"},
		{"marker on a script's closing line", "> <script>\n> Warn </script>\n> text\n", "<blockquote>", warning},
		{"marker on the first line still counts", "> <!-- Info\n> Warn -->\n> text\n", info, "<blockquote>"},
		{"one-line comment marker", "> <!-- Info -->\n> text\n", info, "<blockquote>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := CompileMarkdown([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
			assert.NotContains(t, out, tc.not)

			out, _, err = CompileMarkdownLegacy([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
			assert.NotContains(t, out, tc.not)
		})
	}
}
