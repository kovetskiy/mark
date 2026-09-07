package mermaid

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/chrome"
	"github.com/rs/zerolog/log"
)

// The engines a diagram can be drawn by.
const (
	// EngineChrome renders in a headless browser, which is what mark has always
	// done and what mermaid.js itself is built for.
	EngineChrome = "chrome"

	// EngineMerman shells out to the merman CLI, which lays a diagram out
	// natively and needs no browser at all. Experimental: it is a reimplementation
	// rather than mermaid.js, so a diagram may come out differently or not at
	// all, and the ones it cannot draw are not the ones anybody has written down.
	EngineMerman = "merman"
)

var (
	mermaidEngine mermaid.Renderer
	mermaidKind   = EngineChrome
	mermaidMutex  sync.Mutex
)

// UseEngine chooses what diagrams are drawn by, for the rest of the run.
//
// Set once, before anything is published, because the engine is built lazily
// and shared: a diagram already drawn is not drawn again to match. Changing it
// closes whatever was open, so that a run cannot end up with two.
func UseEngine(kind string) error {
	switch kind {
	case "", EngineChrome:
		kind = EngineChrome

	case EngineMerman:
		log.Warn().Msg(
			"the merman render engine is experimental: it is not mermaid.js, " +
				"so a diagram may be drawn differently or not at all",
		)

	default:
		return fmt.Errorf(
			"unknown mermaid engine %q: expected %q or %q", kind, EngineChrome, EngineMerman,
		)
	}

	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidKind == kind {
		return nil
	}

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}

	mermaidKind = kind

	return nil
}

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

// uncapDiagramWidth stops mermaid.js from drawing a diagram as width="100%"
// under a max-width style, and has it state the size it actually drew instead.
//
// That style is a page's answer to a diagram wider than its column, and a page
// is not where these end up: the drawing is uploaded as an attachment and shown
// at the size mark asks for, which the cap then silently overrides. It also
// leaves the file with no width or height of its own, so the size has to be
// recovered from the viewBox.
//
// useMaxWidth is set for each kind of diagram separately -- twenty-seven of
// them at the time of writing, and more with every mermaid release -- so the
// list is taken from mermaid's own defaults rather than written out here, where
// it would silently go stale.
const uncapDiagramWidth = `mermaid.initialize(Object.assign({startOnLoad: false},
	Object.fromEntries(Object.entries(mermaid.mermaidAPI.defaultConfig)
		.filter(([, section]) => section && typeof section === "object" && "useMaxWidth" in section)
		.map(([name]) => [name, {useMaxWidth: false}]))))`

func getMermaidEngine() (mermaid.Renderer, error) {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		return mermaidEngine, nil
	}

	if mermaidKind == EngineMerman {
		return startMerman()
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
	engine, err := mermaid.NewRenderEngine(context.Background(), []string{uncapDiagramWidth}, chrome.AllocatorOptions()...)
	if err != nil {
		return nil, err
	}

	engine.SetRenderTimeout(renderTimeout)

	// Without this a crash is only ever seen as whatever the in-flight render
	// happened to fail with, and one that happens between diagrams is invisible
	// until the next diagram fails. The handler runs on chromedp's event
	// goroutine, so it may only log: calling back into the engine from there
	// deadlocks.
	//
	// Chrome's own, and not on the Renderer interface: merman starts no browser,
	// so it has nothing that can crash between diagrams.
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
// startMerman builds the CLI-backed engine. Called with the mutex held.
//
// The binary is looked for and asked what it is while the engine is being
// built, so a merman that is missing or too old is reported here -- naming the
// setting that asked for it -- rather than as a diagram that would not draw.
func startMerman() (mermaid.Renderer, error) {
	log.Debug().Msg("Setting up global Mermaid renderer (merman)")

	engine, err := mermaid.NewMermanEngine(context.Background())
	if err != nil {
		return nil, fmt.Errorf(
			"unable to start the merman render engine asked for by --mermaid-engine: %w", err,
		)
	}

	engine.SetRenderTimeout(renderTimeout)

	mermaidEngine = engine

	return mermaidEngine, nil
}

func discardEngine(engine mermaid.Renderer) {
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
func render(title string, once func(ctx context.Context, engine mermaid.Renderer) error) error {
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

	err := render(title, func(ctx context.Context, engine mermaid.Renderer) error {
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

	err := render(title, func(ctx context.Context, engine mermaid.Renderer) error {
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
func ProcessMermaidSVG(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, false, scale)
}

// ProcessMermaidWithBundle does the same and keeps the diagram's source inside
// the SVG, in its <desc> element, so that what was published can be opened and
// edited again without the document it came from.
func ProcessMermaidWithBundle(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, true, scale)
}

func processMermaidSVG(title string, mermaidDiagram []byte, bundle bool, scale float64) (attachment.Attachment, error) {
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

	// The scale multiplies the size the page shows the diagram at, not anything
	// in the file: an SVG is the same drawing however large it is displayed,
	// which is why the scale is no part of the checksum above.
	width, height := extractSVGDimensions(svg)
	if scale > 0 {
		width *= scale
		height *= scale
	}

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  title + ".svg",
		FileBytes: []byte(svg),
		Checksum:  checkSum,
		Replace:   title,
		Width:     pixels(width),
		Height:    pixels(height),
	}, nil
}

func boolByte(b bool) byte {
	if b {
		return 1
	}

	return 0
}

// extractSVGDimensions reports the size of a rendered diagram in pixels, which
// is what decides its alignment and display width on the page.
//
// The root element's width and height are preferred, and the viewBox is the
// fallback for either of them -- mermaid draws a diagram wide enough to need
// one as width="100%", which is not a number of pixels and would otherwise be
// read as 100 of them, laying out a wide diagram as a narrow one.
func extractSVGDimensions(svg string) (width, height float64) {
	var attrWidth, attrHeight, attrViewBox string

	decoder := xml.NewDecoder(strings.NewReader(svg))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "svg" {
			continue
		}

		for _, attr := range element.Attr {
			switch attr.Name.Local {
			case "width":
				attrWidth = attr.Value
			case "height":
				attrHeight = attr.Value
			case "viewBox":
				attrViewBox = attr.Value
			}
		}

		break
	}

	width = absoluteLength(attrWidth)
	height = absoluteLength(attrHeight)

	// "minX minY width height", separated by whitespace or commas. Kept as the
	// fallback even though mermaid now states a width and a height of its own:
	// a diagram rendered before that, or by anything else, still may not.
	if (width == 0 || height == 0) && attrViewBox != "" {
		fields := strings.Fields(strings.ReplaceAll(attrViewBox, ",", " "))
		if len(fields) == 4 {
			if width == 0 {
				width = absoluteLength(fields[2])
			}

			if height == 0 {
				height = absoluteLength(fields[3])
			}
		}
	}

	return width, height
}

// absoluteLength reads a length that is a number of pixels -- bare, or written
// with px -- and returns it rounded, or "" for anything else. A relative unit
// (%, em, rem, vw, vh) is not a size on its own, so it is reported as unknown
// rather than as the number in front of it.
func absoluteLength(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if suffix := strings.TrimSuffix(value, "px"); suffix != value {
		value = strings.TrimSpace(suffix)
	} else if last := value[len(value)-1]; last < '0' || last > '9' {
		return 0
	}

	length, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(length) || math.IsInf(length, 0) {
		return 0
	}

	return length
}

// pixels renders a length as the whole number of them an attachment is measured
// in, or "" for a length that was never known. Rounded once, after the scale
// has been applied, since rounding before it would scale a number that had
// already lost the fraction.
func pixels(length float64) string {
	if length == 0 {
		return ""
	}

	return strconv.Itoa(int(math.Round(length)))
}

func Cleanup() {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}
}

// lookMerman reports whether the merman binary can be found, which is what
// decides whether a test has anything to say about it missing.
func lookMerman() (string, error) {
	return exec.LookPath("merman")
}
