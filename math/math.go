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
)

// pixelsPerEx converts the ex units MathJax sizes its output in into the pixels
// Confluence wants on an ac:image.
//
// One ex is the height of a lowercase x, which is about half the font size, so
// 8 is the value for the 16px body text Confluence renders with. Getting this
// wrong does not break anything -- it makes formulas uniformly too large or too
// small beside the text they sit in.
const pixelsPerEx = 8.0

// svgSize pulls the width and height MathJax put on the root element.
var svgSize = regexp.MustCompile(`^<svg[^>]*?\bwidth="([0-9.]+)ex"[^>]*?\bheight="([0-9.]+)ex"`)

// svgError finds the message MathJax draws in place of a formula it could not
// read. It reports a mistake in the source as a rendered box rather than as an
// error, so without this a typo publishes a picture of its own complaint.
var svgError = regexp.MustCompile(`data-mjx-error="([^"]*)"`)

// Process renders one formula and returns it as an attachment.
//
// The name is derived from the formula rather than given by the author: math
// has no equivalent of a diagram's title, and two identical formulas on a page
// should be one attachment. display selects the same distinction TeX makes --
// an inline formula is set compactly, a display one is given room.
func Process(tex string, display bool) (attachment.Attachment, error) {
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

	// The name has to distinguish inline from display: the same formula set
	// both ways is two different images, and a name that ignored that would
	// have the second one overwrite the first.
	seed := tex
	if display {
		seed = "display:" + tex
	}

	checksum, err := attachment.GetChecksum(strings.NewReader(seed))
	if err != nil {
		return attachment.Attachment{}, err
	}

	name := "math-" + checksum[:12]

	return attachment.Attachment{
		Name:      name,
		Filename:  name + ".svg",
		FileBytes: []byte(svg),
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
	if len(groups) != 3 {
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

	width, err = toPixels(groups[1])
	if err != nil {
		return "", "", err
	}

	height, err = toPixels(groups[2])
	if err != nil {
		return "", "", err
	}

	return width, height, nil
}
