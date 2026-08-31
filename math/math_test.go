package math

import (
	"bytes"
	"image/png"
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
	rendered, err := Process(`E = mc^2`, false, FormatSVG, 0)
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
	rendered, err := Process(`E = mc^2`, false, FormatSVG, 0)
	require.NoError(t, err)

	assert.Equal(t, "70", rendered.Width)
	assert.Equal(t, "17", rendered.Height)
}

// TestProcessDisplayAndInlineDiffer covers TeX's own distinction. A display
// formula is set larger and with room around it, so the two are different
// images and must not collapse onto one name -- the second would overwrite the
// first and both occurrences would show the same rendering.
func TestProcessDisplayAndInlineDiffer(t *testing.T) {
	inline, err := Process(`\sum_{i=1}^{n} i`, false, FormatSVG, 0)
	require.NoError(t, err)

	display, err := Process(`\sum_{i=1}^{n} i`, true, FormatSVG, 0)
	require.NoError(t, err)

	assert.NotEqual(t, inline.Filename, display.Filename)
	assert.NotEqual(t, inline.Checksum, display.Checksum)
	assert.NotEqual(t, string(inline.FileBytes), string(display.FileBytes))
}

// TestProcessIsDeterministic covers the reason a formula can be addressed by
// its source at all: the same input renders to the same bytes, so a page that
// has not changed does not re-upload its formulas.
func TestProcessIsDeterministic(t *testing.T) {
	first, err := Process(`\frac{a}{b}`, false, FormatSVG, 0)
	require.NoError(t, err)

	second, err := Process(`\frac{a}{b}`, false, FormatSVG, 0)
	require.NoError(t, err)

	assert.Equal(t, first.Filename, second.Filename)
	assert.Equal(t, first.Checksum, second.Checksum)
	assert.Equal(t, string(first.FileBytes), string(second.FileBytes))
}

// TestProcessNamesTheFileAfterTheFormula covers the naming: authors do not name
// formulas the way they name diagrams, so the name comes from the content, and
// two different formulas must not land on one file.
func TestProcessNamesTheFileAfterTheFormula(t *testing.T) {
	one, err := Process(`a + b`, false, FormatSVG, 0)
	require.NoError(t, err)

	other, err := Process(`a - b`, false, FormatSVG, 0)
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
			_, err := Process(tt.tex, false, FormatSVG, 0)
			require.Error(t, err)

			assert.Contains(t, err.Error(), tt.tex, "the error has to name the formula")
			assert.Contains(t, err.Error(), tt.message, "and what MathJax objected to")
		})
	}
}

// TestProcessPNG covers the other format. It exists for instances that will not
// display an SVG attachment, and it is the only part of the math feature that
// needs a browser -- the same one the diagram renderers use.
func TestProcessPNG(t *testing.T) {
	rendered, err := Process(`E = mc^2`, false, FormatPNG, 2)
	require.NoError(t, err)

	assert.True(t, strings.HasSuffix(rendered.Filename, ".png"))
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, rendered.FileBytes[:8],
		"the attachment has to be a PNG, whatever the name says")

	image, err := png.Decode(bytes.NewReader(rendered.FileBytes))
	require.NoError(t, err)

	// The picture carries scale times the pixels, and the ac:image still claims
	// the space the formula asked for: that is what makes it sharp on a display
	// that can use them rather than merely large.
	assert.Equal(t, "70", rendered.Width)
	assert.Equal(t, "17", rendered.Height)
	assert.Equal(t, 140, image.Bounds().Dx())
	assert.Equal(t, 34, image.Bounds().Dy())
}

// TestProcessScaleChangesTheName covers a document that changes --math-scale.
// The bytes change, so the name has to, or the page keeps the attachment it
// already had and the setting appears to do nothing.
func TestProcessScaleChangesTheName(t *testing.T) {
	one, err := Process(`E = mc^2`, false, FormatPNG, 1)
	require.NoError(t, err)

	two, err := Process(`E = mc^2`, false, FormatPNG, 2)
	require.NoError(t, err)

	assert.NotEqual(t, one.Filename, two.Filename)

	// An SVG is the same file at any scale, so there the setting is ignored
	// rather than allowed to churn the attachment.
	vectorOne, err := Process(`E = mc^2`, false, FormatSVG, 1)
	require.NoError(t, err)

	vectorTwo, err := Process(`E = mc^2`, false, FormatSVG, 4)
	require.NoError(t, err)

	assert.Equal(t, vectorOne.Filename, vectorTwo.Filename)
}

// TestProcessFormats covers the values the flag accepts, and the ones it does
// not: an unknown format has to be an error rather than a silent fallback,
// which would publish something other than what was asked for.
func TestProcessFormats(t *testing.T) {
	svg, err := Process(`x`, false, FormatSVG, 0)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(svg.Filename, ".svg"))

	// Unset means PNG, which is what Confluence certainly displays. A caller
	// that wants the vector picture has to ask for it.
	unset, err := Process(`x`, false, "", 0)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(unset.Filename, ".png"))

	_, err = Process(`x`, false, "jpeg", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jpeg")
}
