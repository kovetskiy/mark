package mermaid

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/chrome"
	svgpkg "github.com/kovetskiy/mark/v16/svg"
	"github.com/rs/zerolog/log"
)

var (
	mermaidEngine *mermaid.RenderEngine
	mermaidMutex  sync.Mutex
)

// renderTimeout bounds a single diagram: the wait for any render already
// occupying the engine's one page, plus the render itself.
//
// It replaces mermaid.go's own DefaultRenderTimeout of 30 seconds, which is too
// tight for the CPU-starved CI containers mark runs in -- the same contention
// the chrome package raises WSURLReadTimeout for.
var renderTimeout = 120 * time.Second

// renderAttempts is how many times one diagram is rendered before giving up.
// A crashed browser is the only failure worth retrying, and only because the
// second attempt runs on a new one: see the crash case in renderPNG.
const renderAttempts = 2

func getMermaidEngine() (*mermaid.RenderEngine, error) {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		return mermaidEngine, nil
	}

	log.Debug().Msg("Setting up global Mermaid renderer")
	// NewRenderEngine prepends chromedp.DefaultExecAllocatorOptions itself, so
	// only the additional options are passed here. Without them Chrome fails to
	// start wherever the sandbox is unavailable -- the same failure the d2
	// renderer hits, since both drive Chrome through chromedp.
	//
	// The context governs the engine's whole lifetime rather than just its
	// startup, so it deliberately carries no deadline: mermaid.go bounds loading
	// the embedded bundle with DefaultStartupTimeout by itself, whereas a
	// deadline here would close the browser mid-run.
	engine, err := mermaid.NewRenderEngine(context.Background(), nil, chrome.AllocatorOptions()...)
	if err != nil {
		return nil, err
	}

	engine.SetRenderTimeout(renderTimeout)

	// Without this a crash is only ever seen as whatever the in-flight render
	// happened to fail with, and one that happens between diagrams is invisible
	// until the next diagram fails. The handler runs on chromedp's event
	// goroutine, so it may only log: calling back into the engine from there
	// deadlocks.
	engine.SetTargetCrashedHandler(func(err error) {
		log.Error().Err(err).Msg("Chrome crashed while rendering Mermaid diagrams")
	})

	mermaidEngine = engine
	return mermaidEngine, nil
}

// discardEngine drops engine from the global slot and closes it, so that the
// next diagram launches a new browser. It is a no-op when the slot has already
// moved on, so that a second diagram failing against the same dead engine
// cannot tear down the replacement the first one built.
func discardEngine(engine *mermaid.RenderEngine) {
	mermaidMutex.Lock()
	if mermaidEngine == engine {
		mermaidEngine = nil
	}
	mermaidMutex.Unlock()

	engine.Cancel()
}

// render runs one diagram through the shared engine, deciding from mermaid.go's
// sentinel errors whether the engine survived the failure and whether another
// attempt is worth making. What to render with it is left to the caller, since
// a PNG and an SVG differ in nothing else.
func render(title string, once func(ctx context.Context, engine *mermaid.RenderEngine) error) error {
	for attempt := 1; ; attempt++ {
		engine, err := getMermaidEngine()
		if err != nil {
			return err
		}

		// The context bounds the wait for a turn on the engine's page as well as
		// the render, which the engine's own timeout does not: that clock only
		// starts once the render begins.
		ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
		err = once(ctx, engine)
		cancel()

		switch {
		case err == nil:
			return nil

		case errors.Is(err, mermaid.ErrTargetCrashed), errors.Is(err, mermaid.ErrEngineClosed):
			// Every later render on this engine fails the same way, so it is
			// closed here and the next attempt gets a fresh browser. This is the
			// one failure a retry can fix.
			discardEngine(engine)
			if attempt < renderAttempts {
				log.Warn().Err(err).Msgf("Mermaid render engine died on %q, retrying with a new browser", title)
				continue
			}
			return err

		case errors.Is(err, mermaid.ErrRenderException):
			// The diagram is what failed, so the engine is still good and a
			// retry would fail identically. mermaid.go keeps chrome's
			// *runtime.ExceptionDetails in the chain, so the message already
			// names the mermaid parse error.
			return fmt.Errorf("invalid mermaid diagram: %w", err)

		case errors.Is(err, context.DeadlineExceeded):
			// Cancelling a render only aborts its in-flight commands, so the
			// engine stays usable and is kept for the next diagram. Retrying is
			// not worth another renderTimeout on a diagram that has already
			// shown it does not settle.
			return fmt.Errorf("mermaid rendering timed out after %v: %w", renderTimeout, err)

		default:
			// An unclassified failure says nothing about whether the browser
			// survived it, so it is discarded: starting the next diagram over is
			// cheap next to producing a page with a diagram missing from it.
			discardEngine(engine)
			return err
		}
	}
}

func renderPNG(title, diagram string, scale float64) ([]byte, *mermaid.BoxModel, error) {
	var (
		pngBytes []byte
		boxModel *mermaid.BoxModel
	)

	err := render(title, func(ctx context.Context, engine *mermaid.RenderEngine) error {
		var err error
		pngBytes, boxModel, err = engine.RenderAsScaledPngContext(ctx, diagram, scale)

		return err
	})

	return pngBytes, boxModel, err
}

// renderSVG renders one diagram as the SVG the browser drew, rather than a
// picture of it. bundle keeps the diagram's own source in the SVG's <desc>
// element, which is what makes the drawing editable again from the attachment.
func renderSVG(title, diagram string, bundle bool) (string, error) {
	var svg string

	err := render(title, func(ctx context.Context, engine *mermaid.RenderEngine) error {
		var err error
		if bundle {
			svg, err = engine.RenderContext(ctx, diagram, mermaid.WithBundle())
		} else {
			svg, err = engine.RenderContext(ctx, diagram)
		}

		return err
	})

	return svg, err
}

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	log.Debug().Msgf("Rendering: %q", title)

	pngBytes, boxModel, err := renderPNG(title, string(mermaidDiagram), scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	mermaidBytes := append(mermaidDiagram, scaleAsBytes...)

	checkSum, err := attachment.GetChecksum(bytes.NewReader(mermaidBytes))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}
	if title == "" {
		title = checkSum
	}

	fileName := title + ".png"

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  fileName,
		FileBytes: pngBytes,
		Checksum:  checkSum,
		Replace:   title,
		Width:     strconv.FormatInt(boxModel.Width, 10),
		Height:    strconv.FormatInt(boxModel.Height, 10),
	}, nil
}

// ProcessMermaidSVG publishes a diagram as the SVG it was drawn as: one file at
// every zoom, and text that stays text. MermaidScale has nothing to multiply
// here and does not apply.
func ProcessMermaidSVG(title string, mermaidDiagram []byte) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, false)
}

// ProcessMermaidWithBundle does the same and keeps the diagram's source inside
// the SVG, in its <desc> element, so that what was published can be opened and
// edited again without the document it came from.
func ProcessMermaidWithBundle(title string, mermaidDiagram []byte) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, true)
}

func processMermaidSVG(title string, mermaidDiagram []byte, bundle bool) (attachment.Attachment, error) {
	log.Debug().Msgf("Rendering SVG (bundle=%v): %q", bundle, title)

	svg, err := renderSVG(title, string(mermaidDiagram), bundle)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// The flag goes into the checksum for the same reason the scale does on the
	// PNG side: the same diagram published with and without its source is two
	// different attachments, and a checksum taken over the diagram alone would
	// call the second one unchanged and leave the first in place.
	checksumInput := make([]byte, 0, len(mermaidDiagram)+1)
	checksumInput = append(checksumInput, mermaidDiagram...)
	checksumInput = append(checksumInput, boolByte(bundle))

	checkSum, err := attachment.GetChecksum(bytes.NewReader(checksumInput))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}

	if title == "" {
		title = checkSum
	}

	width, height := svgpkg.Dimensions(svg)

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  title + ".svg",
		FileBytes: []byte(svg),
		Checksum:  checkSum,
		Replace:   title,
		Width:     svgpkg.Pixels(width),
		Height:    svgpkg.Pixels(height),
	}, nil
}

func boolByte(b bool) byte {
	if b {
		return 1
	}

	return 0
}
func Cleanup() {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}
}
