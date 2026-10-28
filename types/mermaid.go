package types

import (
	"errors"
	"fmt"
	"strings"
)

// MermaidDraws reports whether mark draws the diagram itself: every output but
// "macro" does, and the empty value is the PNG default.
func MermaidDraws(output string) bool {
	return output != "macro"
}

// ValidateMermaidOutput reports a mermaid output name mark does not publish as.
// The empty value is the default (PNG) and is not one.
func ValidateMermaidOutput(output string) error {
	switch output {
	case "", "png", "svg", "macro":
		return nil
	}

	return fmt.Errorf(
		"invalid mermaid output %q: expected \"png\", \"svg\", \"macro\", or \"\" for the default",
		output,
	)
}

// ValidateMermaidBundle reports a bundle that has nowhere to go: only an SVG
// has room for the diagram's source. The caller names the flag or the field it
// came from.
func ValidateMermaidBundle(output string, bundle bool) error {
	if !bundle || output == "svg" {
		return nil
	}

	if output == "macro" {
		return errors.New("the macro publishes the diagram's source itself")
	}

	return errors.New("a PNG has nowhere to keep the diagram's source")
}

// MermaidMacroNameOrDefault is the macro key a diagram is published under: the
// name without surrounding space, and MermaidMacroDefaultName when it is blank.
func MermaidMacroNameOrDefault(name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}

	return MermaidMacroDefaultName
}
