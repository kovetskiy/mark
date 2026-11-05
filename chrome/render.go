package chrome

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
	"github.com/kovetskiy/mark/v17/internal/lazy"
)

// renderTimeout bounds one screenshot. Starting the browser is not part of it:
// that wait is startupTimeout below.
const renderTimeout = 120 * time.Second

// startupTimeout bounds getting a browser ready to be asked for a picture.
//
// chromedp puts no bound on it: once Chrome is running, the handshake over its
// debugging pipe, Target.createTarget and Target.attachToTarget can each wait
// for ever, so a browser that started and then wedged left mark waiting with no
// output and no error. A contended CI box and a stalled GPU-process init both
// produce exactly that.
const startupTimeout = 90 * time.Second

// maxRasterSide is the largest side, in pixels after scaling, that a capture may
// ask for. Chrome will not draw a texture wider than this and the failure it
// gives for trying says nothing useful.
const maxRasterSide = 16384

// maxRasterPixels is the largest area, in pixels after scaling, that a capture
// may ask for. Roughly a quarter of a gigabyte of bitmap, which is far above any
// real diagram and far below what it takes to end the browser.
const maxRasterPixels = 64 << 20

// ErrNoBrowser is what rendering asks of a nil *Browser: the caller drew a
// picture without giving it a browser to draw in.
var ErrNoBrowser = errors.New("no browser to render with")

// errClosed is what a Browser answers once it has been closed.
var errClosed = errors.New("the browser has been closed")

// Browser is the headless Chrome a run renders pictures in, started by the
// first one that needs it.
//
// One browser per run rather than one per image: Chrome takes seconds to start
// and a document with a dozen diagrams or formulas would otherwise spend most
// of its time launching it. Lazily, so that a run with nothing to draw never
// launches it at all.
//
// Whoever creates one closes it. It is safe for concurrent use: renders share
// the browser's one tab, so they take turns in it.
type Browser struct {
	// render is held from navigating to capturing, so that no render sees the
	// page another one loaded.
	render  sync.Mutex
	session *lazy.Lazy[*session]
}

// session is one running browser: the context chromedp drives it through, and
// what shuts it down.
type session struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBrowser returns a Browser that has not started yet.
func NewBrowser() *Browser {
	return &Browser{
		session: lazy.New(startSession, func(s *session) { s.cancel() }, errClosed),
	}
}

// startSession launches a browser that lives as long as life, which Close
// cancels.
func startSession(life context.Context) (*session, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:], AllocatorOptions()...)

	allocCtx, allocCancel := chromedp.NewExecAllocator(life, opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	stop := func() {
		cancel()
		allocCancel()
	}

	// Bounded, but not by giving the browser a context that expires: the
	// browser has to outlive its own startup.
	started := make(chan error, 1)
	go func() { started <- chromedp.Do(ctx) }()

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()

	select {
	case err := <-started:
		if err != nil {
			stop()

			return nil, err
		}

	case <-life.Done():
		stop()

		return nil, context.Cause(life)

	case <-timer.C:
		stop()

		return nil, fmt.Errorf(
			"the browser did not finish starting within %s; "+
				"it announced itself and then stopped responding, "+
				"which usually means the machine is too loaded or too small to run it",
			startupTimeout,
		)
	}

	return &session{ctx: ctx, cancel: stop}, nil
}

// start returns a running browser, starting one if there is none. It is not
// exported because every render navigates the one tab: only PNGFromSVG,
// holding b.render, may use it.
func (b *Browser) start(ctx context.Context) (*session, error) {
	s, err := b.session.Get(ctx)
	if err != nil {
		return nil, err
	}

	if s.ctx.Err() == nil {
		return s, nil
	}

	// chromedp cancels this exact context when it loses the connection to the
	// browser, so a context that is done means Chrome has died. Handed out
	// again, every render after it would fail with a bare "context canceled"
	// that names neither the browser nor the crash.
	b.session.Discard(s)

	return b.session.Get(ctx)
}

// failed discards the browser s, unless another render has already replaced
// it, and returns the render's err. Unlike the mermaid engine, which keeps its
// engine through a timeout, a browser that failed any render is not trusted
// for the next one: the usual cause is that it died, and every later render
// would fail the same way. A render Close interrupted is said to be Close's
// doing, rather than a bare "context canceled".
func (b *Browser) failed(s *session, err error) error {
	b.session.Discard(s)

	if b.session.Closed() {
		return fmt.Errorf("%w: %w", errClosed, err)
	}

	return err
}

// Close shuts the browser down for good, and stops one still starting
// without waiting for it. A Browser that never rendered anything has nothing
// to shut down, and closing twice, or closing nil, is harmless.
func (b *Browser) Close() {
	if b == nil {
		return
	}

	b.session.Close()
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
// does not -- so it is the caller's to give. ctx bounds the render, not the
// browser, which outlives it.
func (b *Browser) PNGFromSVG(ctx context.Context, svg []byte, selector string, scale float64) (png []byte, width, height int64, err error) {
	if b == nil {
		return nil, 0, 0, ErrNoBrowser
	}
	// A render that has already ended neither starts a browser nor discards
	// one.
	if ctx.Err() != nil {
		return nil, 0, 0, context.Cause(ctx)
	}

	b.render.Lock()
	defer b.render.Unlock()

	browser, err := b.start(ctx)
	if err != nil {
		return nil, 0, 0, err
	}

	// Derived from the browser's context, since chromedp runs in the one it
	// is given, and ended with the caller's too.
	runCtx, cancel := context.WithTimeout(browser.ctx, renderTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	// Measured before it is captured. A diagram's dimensions come from the
	// document and are then multiplied by the scale, so four lines of d2 can
	// ask for a capture of well over a gigapixel -- several gigabytes of bitmap
	// -- which ends the browser, and with it every later diagram in the run.
	var model *dom.BoxModel

	err = chromedp.Do(runCtx,
		chromedp.Navigate(fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(svg))),
	)
	if err == nil {
		model, err = chromedp.Run(runCtx, chromedp.Dimensions(chromedp.JSPath(selector)))
	}

	if err != nil {
		return nil, 0, 0, b.failed(browser, stoppedBy(ctx, err))
	}

	// Not a browser fault, so the browser is kept: the next diagram in the
	// document has done nothing wrong.
	if err := CheckRasterBounds(float64(model.Width), float64(model.Height), scale); err != nil {
		return nil, 0, 0, err
	}

	result, err := chromedp.Run(runCtx, chromedp.ScreenshotScale(chromedp.JSPath(selector), scale))
	if err != nil {
		return nil, 0, 0, b.failed(browser, stoppedBy(ctx, err))
	}

	return result, model.Width, model.Height, nil
}

// stoppedBy is a render's err, or the caller's reason when ctx is what ended
// the render. The run's context is derived from the browser's and only
// cancelled when ctx ends, so chromedp sees a caller's deadline as a bare
// Canceled.
func stoppedBy(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		return err
	}

	return fmt.Errorf("render stopped: %w", context.Cause(ctx))
}

// ErrRasterRefused matches, through errors.Is, every capture CheckRasterBounds
// refuses. It is there so that a caller can tell a diagram that asked for too
// much from a browser that failed: the one is the document's fault and leaves
// the browser fit for the next diagram, the other does not.
var ErrRasterRefused = errors.New("capture refused")

// refusal carries CheckRasterBounds' own message, which already says what to
// do, while still matching ErrRasterRefused.
type refusal struct{ error }

func (refusal) Is(target error) bool { return target == ErrRasterRefused }

// CheckRasterBounds refuses a capture too large for the browser to survive
// being asked for. width and height are the element's layout size, before
// scale.
//
// Reported against what was asked for rather than what came back, because
// nothing comes back: the browser is gone, and so is every diagram after it.
//
// Every number is checked for being one before it is compared. NaN fails every
// comparison it is given, so a bound written as "more than the limit" lets a
// NaN scale through to the screenshot, and a NaN size multiplied by anything
// is NaN as well.
func CheckRasterBounds(width, height, scale float64) error {
	if !(scale > 0) || math.IsInf(scale, 0) {
		return refusal{fmt.Errorf("scale %v is not a finite number greater than 0", scale)}
	}

	if !(width >= 0) || !(height >= 0) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return refusal{fmt.Errorf("the diagram measures %vx%v, which is not a size in pixels", width, height)}
	}

	scaledWidth := width * scale
	scaledHeight := height * scale

	if scaledWidth > maxRasterSide || scaledHeight > maxRasterSide {
		return refusal{fmt.Errorf(
			"the diagram is %.0fx%.0f pixels at scale %g, and no side may exceed %d; "+
				"make the diagram smaller or lower the scale",
			scaledWidth, scaledHeight, scale, maxRasterSide,
		)}
	}

	if scaledWidth*scaledHeight > maxRasterPixels {
		return refusal{fmt.Errorf(
			"the diagram is %.0fx%.0f pixels at scale %g, which is %.0f million pixels "+
				"and more than the %.0f million a page may hold; "+
				"make the diagram smaller or lower the scale",
			scaledWidth, scaledHeight, scale,
			scaledWidth*scaledHeight/1e6, float64(maxRasterPixels)/1e6,
		)}
	}

	return nil
}
