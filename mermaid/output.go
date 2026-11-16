package mermaid

import (
	"fmt"
	"strings"
)

// Output is how a mermaid diagram is published: as an image mark draws, or as
// the instance's own macro, which draws the source it is given.
type Output string

// The outputs a mermaid diagram can be published as.
const (
	// OutputPNG is a raster image, drawn by mark. It is the default.
	OutputPNG Output = "png"

	// OutputSVG is the drawing itself, drawn by mark and attached as an SVG.
	OutputSVG Output = "svg"

	// OutputMacro publishes the diagram's source as the instance's Mermaid
	// macro, which draws it; mark draws nothing.
	OutputMacro Output = "macro"
)

// Valid reports whether o is an output mark publishes as. The zero value is
// not valid here: whether it means the default is the caller's to say.
func (o Output) Valid() bool {
	return o == OutputPNG || o == OutputSVG || o == OutputMacro
}

// Draws reports whether mark draws the diagram itself: every output but Macro
// does, and the zero value is the PNG default.
func (o Output) Draws() bool {
	return o != OutputMacro
}

// ParseOutput reads an output a person wrote: trimmed and lowercased, so that
// " Macro " is Macro. Anything but png, svg or macro, the empty string
// included, is refused.
func ParseOutput(value string) (Output, error) {
	output := Output(strings.ToLower(strings.TrimSpace(value)))
	if !output.Valid() {
		return "", fmt.Errorf(
			"unknown mermaid output %q: expected %s, %s or %s",
			value, OutputPNG, OutputSVG, OutputMacro,
		)
	}

	return output, nil
}

// DefaultMacroName is the ac:name of the Confluence macro a diagram is
// published as with OutputMacro, when the caller does not name one of their
// own.
const DefaultMacroName = "mermaid-macro"

// MacroNameOrDefault is the macro key a diagram is published under: the name
// without surrounding space, and DefaultMacroName when it is blank.
func MacroNameOrDefault(name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}

	return DefaultMacroName
}
