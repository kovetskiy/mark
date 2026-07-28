package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ac:box/ac:expand/ac:panel/ac:column templates concatenated the body
// directly against </ac:rich-text-body>, so a body ending in a list absorbed the
// closing macro tags into its last <li> and the </li></ul> escaped outside the
// macro -- invalid XML that Confluence rejects. A table body was not parsed as a
// table at all. ac:details had already been fixed this way; these four had not.
func TestMacroRichTextBodyClosesOutsideBlockContent(t *testing.T) {
	tests := []struct {
		name     string
		template string
		params   string
	}{
		{"ac:box", "ac:box", "     Name: info"},
		{"ac:expand", "ac:expand", "     Title: T"},
		{"ac:panel", "ac:panel", "     Title: T"},
		{"ac:column", "ac:column", "     Width: 50%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "<!-- Macro: :m:\n     Template: " + tt.template + "\n" + tt.params +
				"\n     Body: \"- one\\n- two\"\n-->\n\n:m:\n"

			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, "<li>one</li>", "the list must be parsed as a list")
			assert.Contains(t, out, "</ul>\n</ac:rich-text-body>",
				"the closing tags must sit outside the list")
			assert.NotContains(t, out, "</ac:rich-text-body></ac:structured-macro></li>",
				"the closing tags must not be absorbed into a list item")
		})
	}
}
