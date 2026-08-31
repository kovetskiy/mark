package mark_test

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileMath compiles with the vector format, which every test that is not
// about the format itself should use: it is the same storage format either way,
// and it does not start a browser.
func compileMath(t *testing.T, markdown string, features ...string) (string, []attachment.Attachment, error) {
	t.Helper()

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	return mark.CompileMarkdown([]byte(markdown), lib, "testdata/test.md", types.MarkConfig{
		Features:   features,
		MathFormat: "svg",
	})
}

// TestMathFeature covers what a formula becomes.
//
// It is an uploaded SVG rather than markup, because Confluence has no math of
// its own and cannot load the stylesheet that KaTeX or MathJax HTML needs. What
// that markup did instead was publish every formula three times -- as MathML,
// as its LaTeX source, and as a pile of unstyled spans -- which is what this
// feature replaced.
func TestMathFeature(t *testing.T) {
	actual, attachments, err := compileMath(t, "An $E = mc^2$ formula.\n", "math")
	require.NoError(t, err)
	assertWellFormed(t, actual)

	require.Len(t, attachments, 1)
	assert.True(t, strings.HasSuffix(attachments[0].Filename, ".svg"))
	assert.Contains(t, actual, `<ri:attachment ri:filename="`+attachments[0].Filename+`"/>`)
	assert.Contains(t, actual, `ac:width="`+attachments[0].Width+`"`)

	assert.NotContains(t, actual, "katex", "no stylesheet-dependent markup survives")
	assert.NotContains(t, actual, "<math", "and no MathML twin")
	assert.NotContains(t, actual, "annotation")
}

// TestMathAltTextCarriesTheSource covers what makes a published formula
// findable: the image's alt text is the LaTeX it was written from, so search
// and screen readers still have something to work with.
func TestMathAltTextCarriesTheSource(t *testing.T) {
	actual, _, err := compileMath(t, `A matrix $\begin{pmatrix} a & b \end{pmatrix}$ here.`+"\n", "math")
	require.NoError(t, err)
	assertWellFormed(t, actual)

	// Escaped, because the formula is interpolated into an attribute and TeX is
	// made of the characters that would end one.
	assert.Contains(t, actual, `ac:alt="\begin{pmatrix} a &amp; b \end{pmatrix}"`)
}

// TestMathDisplayAndInlineAreDifferentImages covers TeX's own distinction
// surviving the trip: a display formula is set larger than the same formula
// inline, so the two cannot share one attachment.
func TestMathDisplayAndInlineAreDifferentImages(t *testing.T) {
	_, attachments, err := compileMath(t, "Inline $\\sum_{i=1}^{n} i$ and display:\n\n$$\\sum_{i=1}^{n} i$$\n", "math")
	require.NoError(t, err)

	require.Len(t, attachments, 2)
	assert.NotEqual(t, attachments[0].Filename, attachments[1].Filename)
}

// TestMathRepeatedFormulaIsUploadedOnce covers the deduplication. A symbol
// repeated through a page is one image; uploading it once per occurrence would
// put dozens of identical attachments on the page.
func TestMathRepeatedFormulaIsUploadedOnce(t *testing.T) {
	actual, attachments, err := compileMath(t, "First $x^2$ and again $x^2$ and once more $x^2$.\n", "math")
	require.NoError(t, err)
	assertWellFormed(t, actual)

	require.Len(t, attachments, 1)
	assert.Equal(t, 3, strings.Count(actual, "<ac:image"), "but every occurrence still shows it")
}

// TestMathBrokenFormulaFailsTheRun covers a formula that is not valid TeX.
// MathJax renders its own complaint into the image rather than failing, so
// without the check a typo publishes a picture of the words "Missing close
// brace" in the middle of a sentence.
func TestMathBrokenFormulaFailsTheRun(t *testing.T) {
	_, _, err := compileMath(t, "A broken $\\frac{a$ formula.\n", "math")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "Missing close brace")
}

// TestMathWithoutTheFeatureIsLeftAlone covers the default: with the feature
// off, a formula is text, and a document full of dollar signs is unaffected.
func TestMathWithoutTheFeatureIsLeftAlone(t *testing.T) {
	actual, attachments, err := compileMath(t, "An $E = mc^2$ formula.\n", "mermaid", "mention")
	require.NoError(t, err)

	assert.Contains(t, actual, "$E = mc^2$")
	assert.Empty(t, attachments)
}

// TestMathDoesNotEatPrices covers the parser rule that matters most in ordinary
// documents: a sentence about money is not mathematics.
func TestMathDoesNotEatPrices(t *testing.T) {
	actual, attachments, err := compileMath(t, "It costs $5 and $7 today.\n", "math")
	require.NoError(t, err)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "It costs $5 and $7 today.")
	assert.Empty(t, attachments)
}

// TestMathPNGFormat covers the default format end to end.
//
// PNG is what Confluence certainly displays, and rasterising costs the same
// browser mermaid already needs, so it is what a document gets unless it asks
// for the vector picture instead.
func TestMathPNGFormat(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	actual, attachments, err := mark.CompileMarkdown([]byte("An $E = mc^2$ formula.\n"), lib, "testdata/test.md",
		types.MarkConfig{Features: []string{"math"}, MathScale: 2})
	require.NoError(t, err)
	assertWellFormed(t, actual)

	require.Len(t, attachments, 1)
	assert.True(t, strings.HasSuffix(attachments[0].Filename, ".png"))
	assert.Contains(t, actual, `<ri:attachment ri:filename="`+attachments[0].Filename+`"/>`)

	// The formula still occupies the space it asked for, whatever the format.
	assert.Contains(t, actual, `ac:width="70"`)
}

// TestMathRejectsAnUnknownFormat covers the value the flag will not take. The
// CLI rejects it up front; this is the second line of that check, for a caller
// that reaches CompileMarkdown directly.
func TestMathRejectsAnUnknownFormat(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	_, _, err = mark.CompileMarkdown([]byte("An $E = mc^2$ formula.\n"), lib, "testdata/test.md",
		types.MarkConfig{Features: []string{"math"}, MathFormat: "jpeg"})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "jpeg")
}
