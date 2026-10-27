package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMermaidDraws(t *testing.T) {
	for output, draws := range map[string]bool{
		"":      true,
		"png":   true,
		"svg":   true,
		"macro": false,
	} {
		assert.Equal(t, draws, MermaidDraws(output), "output %q", output)
	}
}

func TestValidateMermaidOutput(t *testing.T) {
	for _, output := range []string{"", "png", "svg", "macro"} {
		assert.NoError(t, ValidateMermaidOutput(output), "output %q", output)
	}

	for _, output := range []string{"jpeg", " ", "PNG"} {
		assert.Error(t, ValidateMermaidOutput(output), "output %q", output)
	}
}

func TestValidateMermaidBundle(t *testing.T) {
	assert.NoError(t, ValidateMermaidBundle("svg", true))
	assert.NoError(t, ValidateMermaidBundle("png", false))
	assert.NoError(t, ValidateMermaidBundle("macro", false))
	assert.Error(t, ValidateMermaidBundle("png", true))
	assert.Error(t, ValidateMermaidBundle("macro", true))
}

func TestMermaidMacroNameOrDefault(t *testing.T) {
	assert.Equal(t, "mermaid-macro", MermaidMacroNameOrDefault(""))
	assert.Equal(t, "mermaid-macro", MermaidMacroNameOrDefault("  "))
	assert.Equal(t, "mermaid-macro", MermaidMacroNameOrDefault(" mermaid-macro "))
	assert.Equal(t, "my-macro", MermaidMacroNameOrDefault(" my-macro "))
}
