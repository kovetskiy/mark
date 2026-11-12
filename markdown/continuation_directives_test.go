package markdown

import (
	"testing"

	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/kovetskiy/mark/v17/stdlib"
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

			// In a paragraph of its own, as the continuation it was written in.
			assert.Contains(t, out, "<p>PART <em>BODY</em>")
			assert.NotContains(t, out, "Include:")
		})
	}
}

// The math block parser looks for its closing fence outside the code regions,
// so a formula in a footnote's later paragraph is a display formula too.
func TestMathBlockInAContinuationParagraphIsAFormula(t *testing.T) {
	std, err := stdlib.New(t.Context(), nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		t.Context(),
		[]byte("Text[^1].\n\n[^1]: first\n\n    $$\n    x^2\n    $$\n"), std, "doc.md",
		Options{Features: []string{"math"}, MathOutput: imageformat.SVG},
	)
	require.NoError(t, err)

	assert.Contains(t, out, `ac:alt="x^2"`)
}
