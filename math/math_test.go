package math

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessRendersSelfContainedSVG covers the property the whole approach
// rests on: the image has to carry its own geometry and its own glyphs.
// Anything that depended on a stylesheet or a font file would render as
// nothing, because a Confluence page loads neither.
func TestProcessRendersSelfContainedSVG(t *testing.T) {
	rendered, err := Process(`E = mc^2`, false)
	require.NoError(t, err)

	svg := string(rendered.FileBytes)
	assert.True(t, strings.HasPrefix(svg, "<svg"), "the attachment is an SVG document")
	assert.Contains(t, svg, "<path", "glyphs are outlines, not font references")
	assert.NotContains(t, svg, "<style")
	assert.NotContains(t, svg, "class=\"katex")
	assert.NotContains(t, svg, "@font-face")
}

// TestProcessSizesInPixels covers the conversion Confluence needs. MathJax
// sizes its output in ex, which ac:image has no notion of; a formula published
// without pixel dimensions is left to whatever default the renderer picks.
func TestProcessSizesInPixels(t *testing.T) {
	rendered, err := Process(`E = mc^2`, false)
	require.NoError(t, err)

	assert.Equal(t, "70", rendered.Width)
	assert.Equal(t, "17", rendered.Height)
}

// TestProcessDisplayAndInlineDiffer covers TeX's own distinction. A display
// formula is set larger and with room around it, so the two are different
// images and must not collapse onto one name -- the second would overwrite the
// first and both occurrences would show the same rendering.
func TestProcessDisplayAndInlineDiffer(t *testing.T) {
	inline, err := Process(`\sum_{i=1}^{n} i`, false)
	require.NoError(t, err)

	display, err := Process(`\sum_{i=1}^{n} i`, true)
	require.NoError(t, err)

	assert.NotEqual(t, inline.Filename, display.Filename)
	assert.NotEqual(t, inline.Checksum, display.Checksum)
	assert.NotEqual(t, string(inline.FileBytes), string(display.FileBytes))
}

// TestProcessIsDeterministic covers the reason a formula can be addressed by
// its source at all: the same input renders to the same bytes, so a page that
// has not changed does not re-upload its formulas.
func TestProcessIsDeterministic(t *testing.T) {
	first, err := Process(`\frac{a}{b}`, false)
	require.NoError(t, err)

	second, err := Process(`\frac{a}{b}`, false)
	require.NoError(t, err)

	assert.Equal(t, first.Filename, second.Filename)
	assert.Equal(t, first.Checksum, second.Checksum)
	assert.Equal(t, string(first.FileBytes), string(second.FileBytes))
}

// TestProcessNamesTheFileAfterTheFormula covers the naming: authors do not name
// formulas the way they name diagrams, so the name comes from the content, and
// two different formulas must not land on one file.
func TestProcessNamesTheFileAfterTheFormula(t *testing.T) {
	one, err := Process(`a + b`, false)
	require.NoError(t, err)

	other, err := Process(`a - b`, false)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(one.Filename, "math-"))
	assert.True(t, strings.HasSuffix(one.Filename, ".svg"))
	assert.NotEqual(t, one.Filename, other.Filename)
	assert.Equal(t, one.Name+".svg", one.Filename)
}

// TestProcessRejectsBrokenTeX covers a formula that is not valid TeX.
//
// MathJax does not fail on one: it draws a box containing its own complaint,
// which would be published as the formula and read as part of the document.
// mark treats it the way it treats a diagram that will not compile -- an error
// naming the formula and what was wrong with it.
func TestProcessRejectsBrokenTeX(t *testing.T) {
	tests := []struct {
		name    string
		tex     string
		message string
	}{
		{"unclosed brace", `\frac{a`, "Missing close brace"},
		{"unknown command", `\notacommand{x}`, "Undefined control sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Process(tt.tex, false)
			require.Error(t, err)

			assert.Contains(t, err.Error(), tt.tex, "the error has to name the formula")
			assert.Contains(t, err.Error(), tt.message, "and what MathJax objected to")
		})
	}
}
