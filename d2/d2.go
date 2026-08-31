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
	"github.com/rs/zerolog/log"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

var renderTimeout = 120 * time.Second

func ProcessD2(title string, d2Diagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return attachment.Attachment{}, err
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
		return attachment.Attachment{}, err
	}

	out, err := d2svg.Render(diagram, renderOpts)
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

// Cleanup shuts down the browser this package renders through.
//
// It is kept here as well as in chrome/ because the tests in this package and
// in markdown/ call it by name, and because a caller that only knows it renders
// diagrams should not have to know which package owns the browser.
func Cleanup() {
	chrome.Cleanup()
}
