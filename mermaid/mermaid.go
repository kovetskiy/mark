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

// renderPNG renders one diagram, deciding from mermaid.go's sentinel errors
// whether the engine survived the failure and whether another attempt is worth
// making.
func renderPNG(title, diagram string, scale float64) ([]byte, *mermaid.BoxModel, error) {
	for attempt := 1; ; attempt++ {
		engine, err := getMermaidEngine()
		if err != nil {
			return nil, nil, err
		}

		// The context bounds the wait for a turn on the engine's page as well as
		// the render, which the engine's own timeout does not: that clock only
		// starts once the render begins.
		ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
		pngBytes, boxModel, err := engine.RenderAsScaledPngContext(ctx, diagram, scale)
		cancel()

		switch {
		case err == nil:
			return pngBytes, boxModel, nil

		case errors.Is(err, mermaid.ErrTargetCrashed), errors.Is(err, mermaid.ErrEngineClosed):
			// Every later render on this engine fails the same way, so it is
			// closed here and the next attempt gets a fresh browser. This is the
			// one failure a retry can fix.
			discardEngine(engine)
			if attempt < renderAttempts {
				log.Warn().Err(err).Msgf("Mermaid render engine died on %q, retrying with a new browser", title)
				continue
			}
			return nil, nil, err

		case errors.Is(err, mermaid.ErrRenderException):
			// The diagram is what failed, so the engine is still good and a
			// retry would fail identically. mermaid.go keeps chrome's
			// *runtime.ExceptionDetails in the chain, so the message already
			// names the mermaid parse error.
			return nil, nil, fmt.Errorf("invalid mermaid diagram: %w", err)

		case errors.Is(err, context.DeadlineExceeded):
			// Cancelling a render only aborts its in-flight commands, so the
			// engine stays usable and is kept for the next diagram. Retrying is
			// not worth another renderTimeout on a diagram that has already
			// shown it does not settle.
			return nil, nil, fmt.Errorf("mermaid rendering timed out after %v: %w", renderTimeout, err)

		default:
			// An unclassified failure says nothing about whether the browser
			// survived it, so it is discarded: starting the next diagram over is
			// cheap next to producing a page with a diagram missing from it.
			discardEngine(engine)
			return nil, nil, err
		}
	}
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

func Cleanup() {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}
}
