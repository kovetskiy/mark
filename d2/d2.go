package d2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	stdhtml "html"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/chrome"
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

// ErrUnsafeDiagram is returned for a diagram that would do something when it is
// rendered or opened, rather than depict something.
var ErrUnsafeDiagram = errors.New("diagram is not safe to publish")

var renderTimeout = 120 * time.Second

// diagramPad is the margin d2 draws around a diagram. It is named because the
// size of the picture depends on it: the drawing is the bounding box of what
// was laid out, with this much added on every side.
const diagramPad = 5

// renderSVG compiles a diagram and draws it, which is where both outputs start:
// the PNG is a screenshot of this, and the SVG is this.
//
// The size comes back with it, because d2 knows it: the drawing is the bounding
// box of what it laid out plus the pad on every side. Reading it back out of
// the rendered XML would be parsing an answer that was already given -- and
// mermaid has to do exactly that, since its diagrams are drawn by a browser
// rather than by a library that will say.
func renderSVG(ctx context.Context, d2Diagram []byte) (out []byte, width, height int, err error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, 0, 0, err
	}
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(diagramPad)),
		ThemeID: &d2themescatalog.GrapeSoda.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}

	diagram, _, err := d2lib.Compile(ctx, string(d2Diagram), compileOpts, renderOpts)
	if err != nil {
		return nil, 0, 0, err
	}

	out, err = d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, 0, 0, err
	}

	// Before anything is done with the drawing, because both things done with
	// it are dangerous. The PNG is taken by navigating a browser to this as a
	// document, so a script in it runs here, on the machine publishing -- in a
	// browser started with --no-sandbox. The SVG is uploaded whole, so the same
	// script is served to whoever opens the page.
	if err := checkDrawingIsSafe(out); err != nil {
		return nil, 0, 0, err
	}

	topLeft, bottomRight := diagram.BoundingBox()

	return out,
		bottomRight.X - topLeft.X + 2*diagramPad,
		bottomRight.Y - topLeft.Y + 2*diagramPad,
		nil
}

func ProcessD2(title string, d2Diagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, _, _, err := renderSVG(ctx, d2Diagram)
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
func ProcessD2SVG(title string, d2Diagram []byte, inputPath string, scale float64, bundleRemote bool) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, width, height, err := renderSVG(ctx, d2Diagram)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// A PNG carries what a diagram references by being a picture of it. An SVG
	// carries the reference itself, and Confluence serves the attachment from
	// its own host, where a path relative to the document resolves to nothing
	// and a remote image may be blocked. Both are inlined here, which is what
	// d2's own --bundle does.
	log.Debug().Msgf("Bundling what the diagram references: %q", title)

	// Every reference is judged before any of them is fetched, because the
	// bundler decides for itself what a name means: it reads an absolute path
	// as written, joins a relative one to wherever it was told the document is,
	// and fetches any URL with a client that follows redirects to anywhere at
	// all. None of that is reachable through its API, so the only place to say
	// no is here, before it is asked.
	local, remote, err := references(out, inputPath, bundleRemote)
	if err != nil {
		return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
	}

	if local {
		out, err = imgbundler.BundleLocal(ctx, bundleLogger{}, inputPath, out, false)
		if err != nil {
			return attachment.Attachment{}, err
		}
	}

	if remote {
		out, err = imgbundler.BundleRemote(ctx, bundleLogger{}, out, false)
		if err != nil {
			return attachment.Attachment{}, err
		}
	}

	// Taken over the drawing rather than over the source it was drawn from,
	// which is what the PNG side and mermaid do. Bundling is why: what the
	// diagram references is now inside the file, so an image that changed at
	// the other end of a URL changes the attachment without changing a line of
	// the source -- and a checksum over the source would call that unchanged
	// and leave the old drawing on the page.
	//
	// The cost is that anything else changing the bytes uploads them again. d2
	// draws the same diagram identically from one run to the next, which the
	// tests pin, so in practice that means a d2 upgrade that moves a line by a
	// pixel: one re-upload, of a drawing that did change.
	checkSum, err := attachment.GetChecksum(bytes.NewReader(out))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}

	if title == "" {
		title = checkSum
	}

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  title + ".svg",
		FileBytes: out,
		Checksum:  checkSum,
		Replace:   title,
		Width:     displayed(width, scale),
		Height:    displayed(height, scale),
	}, nil
}

// image matches the pictures d2 draws into an SVG, the same way d2's own
// bundler finds them.
var image = regexp.MustCompile(`<image href="([^"]+)"`)

// references reports what kinds of thing a drawing points at, and refuses the
// ones this run may not read.
//
// A file is held to the boundary an attachment is held to -- the document's own
// directory or the one mark is running in -- because a diagram naming a file is
// doing what a document does, and "../../../etc/id_rsa" reaches no further in
// one than the other. It needs a document on disk to be relative to: "-" is
// d2's way of writing standard input, and reading a name against the working
// directory instead is how a diagram compiled through the library reaches a
// file nobody offered it.
//
// A URL is refused unless this run asked for remote bundling, because fetching
// one is a request made by the document rather than by the person publishing
// it. The bundler's client follows redirects and declines nothing -- not
// loopback, not link-local, not a cloud metadata address -- and what comes back
// is published inside the drawing.
func references(svg []byte, inputPath string, bundleRemote bool) (local, remote bool, err error) {
	for _, match := range image.FindAllSubmatch(svg, -1) {
		href := stdhtml.UnescapeString(string(match[1]))

		// Already carried by the drawing rather than pointed at by it.
		if strings.HasPrefix(href, "data:") {
			continue
		}

		if parsed, parseErr := url.Parse(href); parseErr == nil &&
			(parsed.Scheme == "http" || parsed.Scheme == "https") {
			if !bundleRemote {
				return false, false, fmt.Errorf(
					"references %q, and fetching what a diagram names is off by default: "+
						"pass --d2-bundle-remote for documents whose diagrams you trust",
					href,
				)
			}

			remote = true

			continue
		}

		if inputPath == "" || inputPath == "-" {
			return false, false, fmt.Errorf(
				"references %q, which needs the document it was written in to be a file on disk",
				href,
			)
		}

		if err := attachment.CheckReadable(filepath.Dir(inputPath), href); err != nil {
			return false, false, fmt.Errorf("references %q: %w", href, err)
		}

		local = true
	}

	return local, remote, nil
}

// displayed reports the size the page should show a length at, as the whole
// number of pixels an attachment is measured in.
//
// Multiplied first and rounded once, since rounding before the scale scales a
// number that has already lost its fraction. Formatted as a float rather than
// converted to an int, because a length past what an int holds does not convert
// -- it becomes whatever the machine makes of it, and clamped to at least one
// pixel that would publish an enormous diagram one pixel wide.
//
// A scale that is not a number to multiply by leaves the length alone: run
// checks it, but ProcessD2SVG is exported and reachable without that.
func displayed(length int, scale float64) string {
	size := float64(length)
	if scale > 0 && !math.IsInf(scale, 0) {
		size *= scale
	}

	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return ""
	}

	return strconv.FormatFloat(math.Max(1, math.Round(size)), 'f', -1, 64)
}

// executable are the elements that run or fetch something of their own, rather
// than drawing. d2 puts a |md | label into the SVG as the author wrote it, so
// what a diagram says here is what ends up in the document.
var executable = map[string]bool{
	"script": true,
	"iframe": true,
	"object": true,
	"embed":  true,
}

// checkDrawingIsSafe refuses a rendered diagram that would do something rather
// than depict something.
//
// A d2 label written as |md | is passed through as markup, and both things mark
// does with the result execute it: the PNG is a screenshot taken by navigating
// a browser to the drawing as a document, and the SVG is uploaded to Confluence
// for other people's browsers to open. A diagram in a pull request could
// therefore read a cloud metadata endpoint from the CI runner, or wait to be
// opened by a colleague.
//
// Refused rather than stripped. A diagram that asked to run something is not a
// diagram somebody drew by accident, and quietly publishing a different one
// than was written is its own kind of wrong.
//
// Read with the lenient HTML tokenizer rather than an XML parser: the drawing
// carries xhtml inside foreignObject, which is how a markdown label is
// represented at all, and strict parsing of somebody else's markup is a way to
// fail on documents that were fine.
func checkDrawingIsSafe(svg []byte) error {
	tokenizer := html.NewTokenizer(bytes.NewReader(svg))

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			// Including io.EOF, which is how a document that held nothing
			// objectionable ends.
			return nil

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttributes := tokenizer.TagName()
			if executable[strings.ToLower(string(name))] {
				return fmt.Errorf(
					"%w: it contains <%s>, which would run when the diagram is rendered or opened",
					ErrUnsafeDiagram, name,
				)
			}

			for hasAttributes {
				var key, value []byte

				key, value, hasAttributes = tokenizer.TagAttr()
				if err := checkAttribute(string(key), string(value)); err != nil {
					return err
				}
			}
		}
	}
}

// checkAttribute refuses the two ways an attribute runs something: by being an
// event handler, and by naming a URL that is code rather than a picture.
func checkAttribute(key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))

	if strings.HasPrefix(key, "on") {
		return fmt.Errorf(
			"%w: it sets %s, which would run when the diagram is rendered or opened",
			ErrUnsafeDiagram, key,
		)
	}

	if key != "href" && key != "src" && key != "xlink:href" {
		return nil
	}

	scheme, _, found := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ":")
	if !found {
		// A relative reference, which the bundler decides about separately.
		return nil
	}

	switch scheme {
	case "http", "https", "mailto", "#":
		return nil

	case "data":
		// An image the bundler inlined. Anything else a data: URL can carry is
		// a document, which is to say a way to run something.
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:image/") {
			return nil
		}

		return fmt.Errorf("%w: it points at %.40s, which is not a picture", ErrUnsafeDiagram, value)

	default:
		return fmt.Errorf("%w: it points at a %s: address", ErrUnsafeDiagram, scheme)
	}
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
