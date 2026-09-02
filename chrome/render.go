package chrome

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// renderTimeout bounds one screenshot. Starting the browser is not part of it:
// that wait is startupTimeout below.
const renderTimeout = 120 * time.Second

// startupTimeout bounds getting a browser ready to be asked for a picture.
//
// WSURLReadTimeout covers only the wait for Chrome to print the DevTools URL on
// its stderr. Everything after that -- the websocket handshake,
// Target.createTarget, Target.attachToTarget -- had no bound at all, so a
// browser that announced itself and then wedged left mark waiting with no
// output and no error. A contended CI box and a stalled GPU-process init both
// produce exactly that.
//
// Longer than the URL wait it contains, since it is the whole of startup.
const startupTimeout = 90 * time.Second

// maxRasterSide is the largest side, in pixels after scaling, that a capture may
// ask for. Chrome will not draw a texture wider than this and the failure it
// gives for trying says nothing useful.
const maxRasterSide = 16384

// maxRasterPixels is the largest area, in pixels after scaling, that a capture
// may ask for. Roughly a quarter of a gigabyte of bitmap, which is far above any
// real diagram and far below what it takes to end the browser.
const maxRasterPixels = 64 << 20

var (
	browserCtx    context.Context
	browserCancel context.CancelFunc
	browserMutex  sync.Mutex
)

// Context returns the browser every caller shares, starting it on first use.
//
// One browser per run rather than one per image: Chrome takes seconds to start
// and a document with a dozen diagrams or formulas would otherwise spend most
// of its time launching it.
func Context() (context.Context, error) {
	browserMutex.Lock()
	defer browserMutex.Unlock()

	if browserCtx != nil {
		if browserCtx.Err() == nil {
			return browserCtx, nil
		}

		// chromedp cancels this exact context when it loses the connection to
		// the browser, so a context that is done means Chrome has died. It was
		// handed out regardless, on a nil check alone, and every render after
		// that failed with a bare "context canceled" -- which names neither the
		// browser nor the crash, and never came right, because nothing started
		// another one. Discarded and replaced instead, which is what the
		// mermaid engine has always done for itself.
		discard()
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:], AllocatorOptions()...)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	// Bounded, but not by giving the browser a context that expires: the
	// browser has to outlive its own startup.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(ctx) }()

	select {
	case err := <-started:
		if err != nil {
			cancel()
			allocCancel()

			return nil, err
		}

	case <-time.After(startupTimeout):
		cancel()
		allocCancel()

		return nil, fmt.Errorf(
			"the browser did not finish starting within %s; "+
				"it announced itself and then stopped responding, "+
				"which usually means the machine is too loaded or too small to run it",
			startupTimeout,
		)
	}

	browserCtx = ctx
	browserCancel = func() {
		cancel()
		allocCancel()
	}

	return browserCtx, nil
}

// discard shuts the current browser down. The caller holds browserMutex.
func discard() {
	if browserCancel != nil {
		browserCancel()
		browserCtx = nil
		browserCancel = nil
	}
}

// Cleanup shuts the browser down. A run that never rendered anything has none
// to shut down, and calling this twice is harmless.
func Cleanup() {
	browserMutex.Lock()
	defer browserMutex.Unlock()

	discard()
}

// PNGFromSVG rasterises an SVG by loading it in the browser and screenshotting
// the element the selector picks out.
//
// The returned width and height are the element's layout size, before scale.
// They are what belongs on an ac:image: the picture is rendered at scale times
// that for a display that can use the pixels, but it should occupy the space
// the SVG asked for.
//
// selector differs by producer -- d2 nests its diagram in an outer svg, MathJax
// does not -- so it is the caller's to give.
func PNGFromSVG(svg []byte, selector string, scale float64) (png []byte, width, height int64, err error) {
	ctx, err := Context()
	if err != nil {
		return nil, 0, 0, err
	}

	runCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	// Measured before it is captured. A diagram's dimensions come from the
	// document and are then multiplied by the scale, so four lines of d2 can
	// ask for a capture of well over a gigapixel -- several gigabytes of bitmap
	// -- which ends the browser, and with it every later diagram in the run.
	var model *dom.BoxModel

	err = chromedp.Run(runCtx,
		chromedp.Navigate(fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(svg))),
		chromedp.Dimensions(selector, &model, chromedp.ByJSPath),
	)
	if err != nil {
		// A browser that failed a run is not trusted for the next one: the
		// usual cause is that it died, and every later render would fail the
		// same way until something restarted it.
		Cleanup()

		return nil, 0, 0, err
	}

	// Not a browser fault, so the browser is kept: the next diagram in the
	// document has done nothing wrong.
	if err := checkRasterBounds(model.Width, model.Height, scale); err != nil {
		return nil, 0, 0, err
	}

	var result []byte

	if err := chromedp.Run(runCtx,
		chromedp.ScreenshotScale(selector, scale, &result, chromedp.ByJSPath),
	); err != nil {
		Cleanup()

		return nil, 0, 0, err
	}

	return result, model.Width, model.Height, nil
}

// checkRasterBounds refuses a capture too large for the browser to survive
// being asked for.
//
// Reported against what was asked for rather than what came back, because
// nothing comes back: the browser is gone, and so is every diagram after it.
func checkRasterBounds(width, height int64, scale float64) error {
	scaledWidth := float64(width) * scale
	scaledHeight := float64(height) * scale

	if scaledWidth > maxRasterSide || scaledHeight > maxRasterSide {
		return fmt.Errorf(
			"the diagram is %.0fx%.0f pixels at scale %g, and no side may exceed %d; "+
				"make the diagram smaller or lower the scale",
			scaledWidth, scaledHeight, scale, maxRasterSide,
		)
	}

	if scaledWidth*scaledHeight > maxRasterPixels {
		return fmt.Errorf(
			"the diagram is %.0fx%.0f pixels at scale %g, which is %.0f million pixels "+
				"and more than the %.0f million a page may hold; "+
				"make the diagram smaller or lower the scale",
			scaledWidth, scaledHeight, scale,
			scaledWidth*scaledHeight/1e6, float64(maxRasterPixels)/1e6,
		)
	}

	return nil
}
