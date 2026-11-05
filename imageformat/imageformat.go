// Package imageformat names the two images a rendered picture -- a d2 diagram,
// a mermaid diagram or a formula -- can be published as. It imports nothing of
// mark's, so that the renderers, the Markdown compiler and the command line
// can all share it.
package imageformat

import (
	"fmt"
	"strings"
)

// Format is an image a picture is published as. mark's settings read the zero
// value as PNG.
type Format string

const (
	// PNG is a raster image, drawn by a headless browser. It is the default:
	// every Confluence instance displays it.
	PNG Format = "png"

	// SVG is the drawing itself: sharp at any zoom, where the instance
	// displays an SVG attachment.
	SVG Format = "svg"
)

// Valid reports whether f is PNG or SVG, compared as written. The zero value
// is not valid here: whether it means the default is the caller's to say.
func (f Format) Valid() bool {
	return f == PNG || f == SVG
}

// Parse reads a format a person wrote: trimmed and lowercased, so that
// " SVG " is SVG. Anything but png or svg, the empty string included, is
// refused.
func Parse(value string) (Format, error) {
	format := Format(strings.ToLower(strings.TrimSpace(value)))
	if !format.Valid() {
		return "", fmt.Errorf("unknown image format %q: expected %s or %s", value, SVG, PNG)
	}

	return format, nil
}
