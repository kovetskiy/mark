// Package math renders LaTeX formulas to SVG images that Confluence can show.
//
// Confluence has no math of its own, and the usual answer for Markdown tools --
// KaTeX or MathJax HTML -- does not survive the trip. That markup is a pile of
// positioned <span>s whose layout lives entirely in a stylesheet the page never
// loads, published alongside a MathML twin and the LaTeX source, so a reader
// sees the formula spelled out three times and laid out none of them. An SVG
// carries its own geometry and its glyphs as outlines: it needs no stylesheet,
// no fonts, and no plugin on the instance.
package math

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	mathjax "github.com/d2lang/mathjax-go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/chrome"
)

// The two shapes a formula can be published as.
//
// PNG is the default: it is what Confluence certainly displays, and it is what
// the diagram renderers have always produced, so a page full of formulas
// behaves like a page full of diagrams. Rasterising costs a browser, which is
// no new dependency -- mermaid is on by default and needs the same one.
//
// SVG is the better picture where an instance displays it: vector, sharp at any
// zoom, a few kilobytes, and rendered without a browser at all.
const (
	FormatSVG = "svg"
	FormatPNG = "png"
)

// pixelsPerEx converts the ex units MathJax sizes its output in into the pixels
// Confluence wants on an ac:image.
//
// One ex is the height of a lowercase x, which is about half the font size, so
// 8 is the value for the 16px body text Confluence renders with. Getting this
// wrong does not break anything -- it makes formulas uniformly too large or too
// small beside the text they sit in.
const pixelsPerEx = 8.0

// svgSize picks the width and height MathJax put on the root element out of
// their surroundings, so that they can be read and also rewritten in place --
// replacing the whole prefix would drop the xmlns, and a browser will not parse
// an SVG document without one.
var svgSize = regexp.MustCompile(`^(<svg[^>]*?\bwidth=")([0-9.]+)ex("[^>]*?\bheight=")([0-9.]+)ex(")`)

// svgError finds the message MathJax draws in place of a formula it could not
// read. It reports a mistake in the source as a rendered box rather than as an
// error, so without this a typo publishes a picture of its own complaint.
var svgError = regexp.MustCompile(`data-mjx-error="([^"]*)"`)

// defaultScale is what a PNG formula is rasterised at when nothing says
// otherwise. Formulas are small, and a 1:1 rasterisation of one looks ragged
// beside the text it sits in.
const defaultScale = 2.0

// Process renders one formula and returns it as an attachment.
//
// The name is derived from the formula rather than given by the author: math
// has no equivalent of a diagram's title, and two identical formulas on a page
// should be one attachment. display selects the same distinction TeX makes --
// an inline formula is set compactly, a display one is given room.
func Process(tex string, display bool, format string, scale float64) (attachment.Attachment, error) {
	switch format {
	case "":
		format = FormatPNG
	case FormatSVG, FormatPNG:
	default:
		return attachment.Attachment{}, fmt.Errorf("unknown math format %q, expected %q or %q", format, FormatSVG, FormatPNG)
	}

	if scale <= 0 {
		scale = defaultScale
	}

	svg, err := mathjax.RenderWithOptions(tex, displayOptions(display))
	if err != nil {
		return attachment.Attachment{}, fmt.Errorf("unable to render formula %q: %w", tex, err)
	}

	if message := svgError.FindStringSubmatch(svg); message != nil {
		return attachment.Attachment{}, fmt.Errorf("unable to render formula %q: %s", tex, html.UnescapeString(message[1]))
	}

	width, height, err := sizeInPixels(svg)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// Everything that changes the bytes has to change the name, or a document
	// that switches format or scale keeps the attachment it had. Display is
	// part of it because the same formula set both ways is two images; scale is
	// only part of it for a PNG, since an SVG is the same file at any scale.
	seed := fmt.Sprintf("%s:%v:%s", format, display, tex)
	if format == FormatPNG {
		seed = fmt.Sprintf("%s:%v:%v:%s", format, display, scale, tex)
	}

	checksum, err := attachment.GetChecksum(strings.NewReader(seed))
	if err != nil {
		return attachment.Attachment{}, err
	}

	name := "math-" + checksum[:12]

	contents := []byte(svg)
	if format == FormatPNG {
		contents, err = rasterise(svg, width, height, scale)
		if err != nil {
			return attachment.Attachment{}, fmt.Errorf("unable to rasterise formula %q: %w", tex, err)
		}
	}

	return attachment.Attachment{
		Name:      name,
		Filename:  name + "." + format,
		FileBytes: contents,
		// Addressed by the formula rather than by the rendered bytes, so that a
		// later mathjax-go release changing its output by a hair does not
		// re-upload every formula on every page.
		Checksum: checksum,
		Replace:  name,
		Width:    width,
		Height:   height,
	}, nil
}

// displayOptions selects between TeX's two modes.
func displayOptions(display bool) mathjax.Options {
	options := mathjax.DefaultOptions()
	options.Display = display

	return options
}

// sizeInPixels converts the root element's ex dimensions into pixels.
func sizeInPixels(svg string) (width, height string, err error) {
	groups := svgSize.FindStringSubmatch(svg)
	if len(groups) != 6 {
		return "", "", fmt.Errorf("rendered formula has no ex dimensions to convert: %.80s", svg)
	}

	toPixels := func(ex string) (string, error) {
		value, err := strconv.ParseFloat(ex, 64)
		if err != nil {
			return "", fmt.Errorf("unable to read %q as a length: %w", ex, err)
		}

		// Rounded up, so that a formula is never given less room than it needs
		// and clipped.
		return strconv.Itoa(int(value*pixelsPerEx + 0.999)), nil
	}

	width, err = toPixels(groups[2])
	if err != nil {
		return "", "", err
	}

	height, err = toPixels(groups[4])
	if err != nil {
		return "", "", err
	}

	return width, height, nil
}

// rasterise turns the rendered SVG into a PNG.
//
// The width and height are written back onto the root element first, in pixels.
// MathJax sizes its output in ex, which is a font-relative unit, and a browser
// asked to lay out a bare SVG document has no text around it to resolve that
// against; the picture would come out at whatever the default font size makes
// of it rather than at the size the ac:image claims.
//
// scale multiplies the pixels, not the size: the image still occupies the space
// the formula asked for, with more pixels in it for a display that can use
// them. A formula is small enough that a 1:1 rasterisation looks ragged beside
// the text it sits in.
func rasterise(svg, width, height string, scale float64) ([]byte, error) {
	sized := svgSize.ReplaceAllString(svg, "${1}"+width+"${3}"+height+"${5}")

	png, _, _, err := chrome.PNGFromSVG([]byte(sized), `document.querySelector("svg")`, scale)
	if err != nil {
		return nil, err
	}

	return png, nil
}
