package d2

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strconv"
	"time"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/chrome"
	"github.com/kovetskiy/mark/v16/svg"
	"github.com/rs/zerolog/log"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/imgbundler"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

var renderTimeout = 120 * time.Second

// renderSVG compiles a diagram and draws it, which is where both outputs start:
// the PNG is a screenshot of this, and the SVG is this.
func renderSVG(ctx context.Context, d2Diagram []byte) ([]byte, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
	}
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(5)),
		ThemeID: &d2themescatalog.GrapeSoda.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}

	diagram, _, err := d2lib.Compile(ctx, string(d2Diagram), compileOpts, renderOpts)
	if err != nil {
		return nil, err
	}

	return d2svg.Render(diagram, renderOpts)
}

func ProcessD2(title string, d2Diagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, err := renderSVG(ctx, d2Diagram)
	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debug().Msgf("Rendering: %q", title)
	// d2 nests the diagram in an outer svg, so the inner one is what to
	// screenshot: the outer one carries the padding.
	pngBytes, width, height, err := chrome.PNGFromSVG(out, `document.querySelector("svg > svg")`, scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	d2Bytes := append(d2Diagram, scaleAsBytes...)

	checkSum, err := attachment.GetChecksum(bytes.NewReader(d2Bytes))

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
		Width:     strconv.FormatInt(width, 10),
		Height:    strconv.FormatInt(height, 10),
	}, nil
}

// ProcessD2SVG publishes a diagram as the SVG it was drawn as, rather than a
// picture of it: one file that is sharp at any zoom and whose text stays text.
//
// scale multiplies the size the page displays it at. The file itself is the
// same drawing whatever that is, so unlike the PNG's the scale is not part of
// what identifies the attachment.
func ProcessD2SVG(title string, d2Diagram []byte, inputPath string, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, err := renderSVG(ctx, d2Diagram)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// A PNG carries what a diagram references by being a picture of it. An SVG
	// carries the reference itself, and Confluence serves the attachment from
	// its own host, where a path relative to the document resolves to nothing
	// and a remote image may be blocked. Both are inlined here, which is what
	// d2's own --bundle does.
	log.Debug().Msgf("Bundling what the diagram references: %q", title)

	out, err = imgbundler.BundleLocal(ctx, bundleLogger{}, inputPath, out, false)
	if err != nil {
		return attachment.Attachment{}, err
	}

	out, err = imgbundler.BundleRemote(ctx, bundleLogger{}, out, false)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// Taken over the drawing rather than over the source it was drawn from,
	// because what the diagram references is now inside it: an image that
	// changed at the other end of a URL changes the attachment without changing
	// a line of the source, and a checksum over the source would call that
	// unchanged and leave the old one on the page.
	checkSum, err := attachment.GetChecksum(bytes.NewReader(out))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}

	if title == "" {
		title = checkSum
	}

	width, height := svg.Dimensions(string(out))
	if width == 0 || height == 0 {
		log.Warn().Msgf(
			"d2 diagram %q states no size of its own; the page will lay it out without one", title,
		)
	}

	if scale > 0 {
		width *= scale
		height *= scale
	}

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  title + ".svg",
		FileBytes: out,
		Checksum:  checkSum,
		Replace:   title,
		Width:     svg.Pixels(width),
		Height:    svg.Pixels(height),
	}, nil
}

// bundleLogger hands what d2's bundler has to say to mark's own log.
type bundleLogger struct{}

func (bundleLogger) Debug(message string) { log.Debug().Msg(message) }
func (bundleLogger) Info(message string)  { log.Info().Msg(message) }
func (bundleLogger) Error(message string) { log.Error().Msg(message) }

// Cleanup shuts down the browser this package renders through.
//
// It is kept here as well as in chrome/ because the tests in this package and
// in markdown/ call it by name, and because a caller that only knows it renders
// diagrams should not have to know which package owns the browser.
func Cleanup() {
	chrome.Cleanup()
}
