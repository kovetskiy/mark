// Package rendercheck holds the rules for rendering settings that both
// mark.Config's validation and the command line's flag checks apply, so that
// each is written once. What an empty or zero value means differs between the
// two -- a field nobody set, or a flag somebody emptied -- and stays with each.
package rendercheck

import (
	"math"
	"strings"
)

// IsImageFormat reports whether format is one a d2 diagram, a mermaid diagram
// or a formula can be published as. Compared as written, as the renderers
// compare it.
func IsImageFormat(format string) bool {
	return format == "png" || format == "svg"
}

// IsScale reports whether scale is a number a diagram can be multiplied by.
// Written as "> 0" rather than "not <= 0" on purpose: NaN fails every
// comparison it is given, so a check phrased the other way round lets it
// through, and an infinite scale passes one outright.
func IsScale(scale float64) bool {
	return scale > 0 && !math.IsInf(scale, 0)
}

// ImageAlign returns align trimmed and lowercased, and whether that is "left",
// "center" or "right".
func ImageAlign(align string) (string, bool) {
	align = strings.ToLower(strings.TrimSpace(align))

	return align, align == "left" || align == "center" || align == "right"
}
