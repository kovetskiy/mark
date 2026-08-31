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
// that wait is WSURLReadTimeout above.
const renderTimeout = 120 * time.Second

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
		return browserCtx, nil
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:], AllocatorOptions()...)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, err
	}

	browserCtx = ctx
	browserCancel = func() {
		cancel()
		allocCancel()
	}

	return browserCtx, nil
}

// Cleanup shuts the browser down. A run that never rendered anything has none
// to shut down, and calling this twice is harmless.
func Cleanup() {
	browserMutex.Lock()
	defer browserMutex.Unlock()

	if browserCancel != nil {
		browserCancel()
		browserCtx = nil
		browserCancel = nil
	}
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

	var (
		result []byte
		model  *dom.BoxModel
	)

	err = chromedp.Run(runCtx,
		chromedp.Navigate(fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(svg))),
		chromedp.ScreenshotScale(selector, scale, &result, chromedp.ByJSPath),
		chromedp.Dimensions(selector, &model, chromedp.ByJSPath),
	)
	if err != nil {
		// A browser that failed a run is not trusted for the next one: the
		// usual cause is that it died, and every later render would fail the
		// same way until something restarted it.
		Cleanup()
		return nil, 0, 0, err
	}

	return result, model.Width, model.Height, nil
}
