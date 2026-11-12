// Package rendercheck holds the rules for rendering settings that both
// mark.Config's validation and the command line's flag checks apply, so that
// each is written once. What an empty or zero value means differs between the
// two -- a field nobody set, or a flag somebody emptied -- and stays with each.
package rendercheck

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// NoFeatures is the feature name that switches every optional feature off.
const NoFeatures = "none"

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

// Features refuses a list of feature names that says two things at once --
// NoFeatures beside a feature -- or holds a name that is neither NoFeatures
// nor one of known. A name is compared as written, because that is how it is
// read where it takes effect: " mermaid" would go unrecognised there too.
func Features(features, known []string) error {
	if slices.Contains(features, NoFeatures) && len(features) > 1 {
		return errors.New(
			`"` + NoFeatures + `" switches every optional feature off, so it cannot be combined with others`,
		)
	}

	for _, feature := range features {
		if feature != NoFeatures && !slices.Contains(known, feature) {
			return fmt.Errorf(
				"%q (expected any of: %s, or %s)",
				feature, strings.Join(known, ", "), NoFeatures,
			)
		}
	}

	return nil
}
