package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noteMacro = "<!-- Macro: :note:\n     Template: #inline\n     inline: EXPANDED -->\n\n"

// A footnote's or a definition's later paragraph is indented by four spaces
// and published as a paragraph, so a macro there is expanded like one in the
// first paragraph.
func TestMacroInAContinuationParagraphIsExpanded(t *testing.T) {
	for name, body := range map[string]string{
		"footnote":   "Text[^1].\n\n[^1]: first :note:\n\n    second :note: para\n",
		"definition": "Term\n: first :note:\n\n    second :note: para\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := compileBeside(t, noteMacro+body, nil)
			require.NoError(t, err)

			assert.Contains(t, out, "first EXPANDED")
			assert.Contains(t, out, "second EXPANDED para")
			assert.NotContains(t, out, ":note:")
		})
	}
}

func TestMacroInCodeInsideAFootnoteIsLeftAlone(t *testing.T) {
	out, err := compileBeside(t,
		noteMacro+"Text[^1].\n\n[^1]: first :note:\n\n        sample :note:\n", nil,
	)
	require.NoError(t, err)

	assert.Contains(t, out, "first EXPANDED")
	assert.Contains(t, out, "sample :note:")
}

func TestIncludeInAContinuationParagraphIsExpanded(t *testing.T) {
	for name, body := range map[string]string{
		"footnote":   "Text[^1].\n\n[^1]: first\n\n    <!-- Include: part.md -->\n",
		"definition": "Term\n: first\n\n    <!-- Include: part.md -->\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := compileBeside(t, body, map[string]string{"part.md": "PART *BODY*"})
			require.NoError(t, err)

			assert.Contains(t, out, "PART <em>BODY</em>")
			assert.NotContains(t, out, "Include:")
		})
	}
}
