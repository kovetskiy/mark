package chrome

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRasterBoundsRefuseAnOversizedDiagram: a diagram's dimensions come from the
// document and are then multiplied by the scale, so four lines of d2
//
//	big: {shape: rectangle; width: 40000; height: 40000}
//
// compile to a 9 KB SVG asking for a capture of about 1.6 gigapixels -- several
// gigabytes of bitmap. The browser does not survive being asked, and taking it
// down takes every later diagram in the run with it.
func TestRasterBoundsRefuseAnOversizedDiagram(t *testing.T) {
	err := CheckRasterBounds(40012, 40012, 1.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "40012x40012", "the message names the size asked for")
	assert.Contains(t, err.Error(), "scale")
}

// TestRasterBoundsCountTheScale: the scale is what turns a diagram that would
// have fitted into one that does not, so it belongs in the arithmetic and in
// the message.
func TestRasterBoundsCountTheScale(t *testing.T) {
	// Comfortable on its own.
	require.NoError(t, CheckRasterBounds(4000, 4000, 1.0))

	// The same diagram at four times the scale is sixteen times the pixels.
	err := CheckRasterBounds(4000, 4000, 4.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "16000x16000")
	assert.Contains(t, err.Error(), "scale 4")
}

// TestRasterBoundsRefuseALongThinDiagram: area is not the only limit. Chrome
// will not draw a texture wider than maxRasterSide however few pixels it holds,
// and what it says for trying is no help.
func TestRasterBoundsRefuseALongThinDiagram(t *testing.T) {
	err := CheckRasterBounds(20000, 10, 1.0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no side may exceed")
}

// TestRasterBoundsAllowRealDiagrams: the bound exists to stop a runaway, not to
// second-guess anybody's architecture diagram.
func TestRasterBoundsAllowRealDiagrams(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height float64
		scale         float64
	}{
		{"a formula", 320, 64, 2.0},
		{"a sequence diagram", 1200, 2400, 1.0},
		{"a large architecture diagram", 4096, 4096, 1.0},
		{"a wide timeline at double scale", 6000, 1200, 2.0},
	} {
		t.Run(size.name, func(t *testing.T) {
			assert.NoError(t, CheckRasterBounds(size.width, size.height, size.scale))
		})
	}
}

// TestRasterBoundsRefuseWhatIsNotANumber: NaN fails every comparison it is
// given, so a bound written as "more than the limit" let a NaN scale through to
// the screenshot -- where chromedp cannot even encode it -- and a NaN or
// infinite size multiplied by anything is no size either.
func TestRasterBoundsRefuseWhatIsNotANumber(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)

	for _, request := range []struct {
		name                 string
		width, height, scale float64
	}{
		{"a NaN scale", 400, 300, nan},
		{"an infinite scale", 400, 300, inf},
		{"a negatively infinite scale", 400, 300, -inf},
		{"a zero scale", 400, 300, 0},
		{"a negative scale", 400, 300, -1},
		{"a NaN width", nan, 300, 1},
		{"an infinite height", 400, inf, 1},
		{"a negative width", -400, 300, 1},
	} {
		t.Run(request.name, func(t *testing.T) {
			err := CheckRasterBounds(request.width, request.height, request.scale)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrRasterRefused)
		})
	}
}

// TestRasterBoundsRefusalIsRecognisable: a caller keeps its browser when the
// capture was refused, and discards it when the browser failed, so the two
// have to be told apart by more than the message.
func TestRasterBoundsRefusalIsRecognisable(t *testing.T) {
	err := CheckRasterBounds(40012, 40012, 1.0)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRasterRefused)
	assert.NotContains(t, err.Error(), ErrRasterRefused.Error(),
		"the sentinel is for errors.Is, not for the reader")
}

// TestContextReplacesADeadBrowser: chromedp cancels the very context it handed
// back when it loses its connection to the browser, so once Chrome dies the
// singleton is a context that is already done. It was handed out anyway, on a
// nil check alone, and every render for the rest of the run failed with a bare
//
//	context canceled
//
// naming neither the browser nor the crash -- and it never came right, because
// nothing ever started another one.
func TestContextReplacesADeadBrowser(t *testing.T) {
	t.Cleanup(Cleanup)

	first, err := Context()
	require.NoError(t, err)
	require.NoError(t, first.Err())

	// What losing the browser looks like from in here: the context is
	// cancelled, and nothing tidies the singleton up, because whatever died did
	// not go through Cleanup.
	browserMutex.Lock()
	browserCancel()
	browserMutex.Unlock()

	require.Error(t, first.Err(), "the shared context is done")

	second, err := Context()
	require.NoError(t, err)

	assert.NoError(t, second.Err(), "a live browser rather than the dead one")
	assert.NotSame(t, first, second)
}

// TestContextReturnsTheSameLiveBrowser is the other half: a browser that is
// still working is still shared, and is not relaunched per diagram.
func TestContextReturnsTheSameLiveBrowser(t *testing.T) {
	t.Cleanup(Cleanup)

	first, err := Context()
	require.NoError(t, err)

	second, err := Context()
	require.NoError(t, err)

	assert.Same(t, first, second)
}
