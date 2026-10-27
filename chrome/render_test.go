package chrome

import (
	"context"
	"fmt"
	"math"
	"sync"
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

// TestStartReplacesADeadBrowser: chromedp cancels the very context it handed
// back when it loses its connection to the browser, so once Chrome dies the
// Browser holds a context that is already done. Handed out on a nil check
// alone, every render for the rest of the run would fail with a bare
//
//	context canceled
//
// naming neither the browser nor the crash -- and it would never come right,
// because nothing would start another one.
func TestStartReplacesADeadBrowser(t *testing.T) {
	browser := NewBrowser()
	t.Cleanup(browser.Close)

	first, err := browser.start()
	require.NoError(t, err)
	require.NoError(t, first.Err())

	// What losing the browser looks like from in here: the context is
	// cancelled, and nothing tidies the Browser up, because whatever died did
	// not go through Close.
	browser.mu.Lock()
	browser.cancel()
	browser.mu.Unlock()

	require.Error(t, first.Err(), "the shared context is done")

	second, err := browser.start()
	require.NoError(t, err)

	assert.NoError(t, second.Err(), "a live browser rather than the dead one")
	assert.NotSame(t, first, second)
}

// TestStartReturnsTheSameLiveBrowser is the other half: a browser that is
// still working is still shared, and is not relaunched per diagram.
func TestStartReturnsTheSameLiveBrowser(t *testing.T) {
	browser := NewBrowser()
	t.Cleanup(browser.Close)

	first, err := browser.start()
	require.NoError(t, err)

	second, err := browser.start()
	require.NoError(t, err)

	assert.Same(t, first, second)
}

func TestBrowserStartsOnlyWhenUsed(t *testing.T) {
	browser := NewBrowser()
	browser.Close()

	assert.Nil(t, browser.ctx, "closing a browser nothing used starts nothing")
}

func TestClosedBrowserRefusesToRender(t *testing.T) {
	browser := NewBrowser()
	browser.Close()

	_, err := browser.start()
	assert.Error(t, err)
}

// TestRenderInterruptedByCloseSaysSo: Close cancels the context a render in
// flight is using, and the failure that render returns should name Close
// rather than read as a bare "context canceled".
func TestRenderInterruptedByCloseSaysSo(t *testing.T) {
	browser := NewBrowser()
	ctx, cancel := context.WithCancel(context.Background())
	browser.ctx, browser.cancel = ctx, cancel

	assert.Equal(t, context.DeadlineExceeded, browser.failed(ctx, context.DeadlineExceeded),
		"a failure Close had no part in is returned as it is")

	browser.ctx, browser.cancel = ctx, cancel
	browser.Close()

	err := browser.failed(ctx, context.Canceled)
	require.ErrorIs(t, err, errClosed)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNilBrowserSaysSo(t *testing.T) {
	var browser *Browser

	_, _, _, err := browser.PNGFromSVG([]byte("<svg/>"), "svg", 1)
	assert.ErrorIs(t, err, ErrNoBrowser)

	browser.Close()
}

// TestConcurrentRendersEachGetTheirOwnPicture: every render navigates the
// browser's one tab, so two at once could measure or capture the other's SVG.
func TestConcurrentRendersEachGetTheirOwnPicture(t *testing.T) {
	browser := NewBrowser()
	t.Cleanup(browser.Close)

	const renders = 8

	var wg sync.WaitGroup

	widths := make([]int64, renders)
	errs := make([]error, renders)

	for i := range renders {
		wg.Add(1)

		go func() {
			defer wg.Done()

			svg := fmt.Sprintf(
				`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="10"><rect width="100%%" height="100%%"/></svg>`,
				20+10*i,
			)
			_, widths[i], _, errs[i] = browser.PNGFromSVG([]byte(svg), "document.querySelector('svg')", 1)
		}()
	}

	wg.Wait()

	for i := range renders {
		require.NoError(t, errs[i])
		assert.Equal(t, int64(20+10*i), widths[i], "render %d", i)
	}
}
