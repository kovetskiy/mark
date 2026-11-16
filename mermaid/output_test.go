package mermaid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputValid(t *testing.T) {
	for _, output := range []Output{OutputPNG, OutputSVG, OutputMacro} {
		assert.True(t, output.Valid(), "output %q", output)
	}

	for _, output := range []Output{"", "jpeg", "SVG", " macro "} {
		assert.False(t, output.Valid(), "output %q", output)
	}
}

func TestOutputDraws(t *testing.T) {
	for output, draws := range map[Output]bool{
		"":          true,
		OutputPNG:   true,
		OutputSVG:   true,
		OutputMacro: false,
	} {
		assert.Equal(t, draws, output.Draws(), "output %q", output)
	}
}

func TestParseOutput(t *testing.T) {
	for value, want := range map[string]Output{
		"png":    OutputPNG,
		" SVG ":  OutputSVG,
		"Macro":  OutputMacro,
		" macro": OutputMacro,
	} {
		output, err := ParseOutput(value)
		require.NoError(t, err, "value %q", value)
		assert.Equal(t, want, output, "value %q", value)
	}

	for _, value := range []string{"", "jpeg", "macro x"} {
		_, err := ParseOutput(value)
		assert.Error(t, err, "value %q", value)
	}
}

func TestMacroNameOrDefault(t *testing.T) {
	assert.Equal(t, "mermaid-macro", MacroNameOrDefault(""))
	assert.Equal(t, "mermaid-macro", MacroNameOrDefault("  "))
	assert.Equal(t, "mermaid-macro", MacroNameOrDefault(" mermaid-macro "))
	assert.Equal(t, "my-macro", MacroNameOrDefault(" my-macro "))
}
