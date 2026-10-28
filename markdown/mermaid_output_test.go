package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileMarkdownRefusesAnUnknownMermaidOutput covers the one way an
// unchecked value reaches the renderer.
//
// Run validates Config, but CompileMarkdown takes a types.MarkConfig straight
// from a caller and never passes through it -- so a format nobody supports
// would otherwise fall to the PNG branch and publish a picture that was not
// asked for, saying nothing at all.
func TestCompileMarkdownRefusesAnUnknownMermaidOutput(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Title\n\n```mermaid\ngraph TD;\n A-->B;\n```\n")

	_, _, err = CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
		Features:      []string{"mermaid"},
		MermaidOutput: "jpeg",
		MermaidScale:  1,
	})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "mermaid-output")
	assert.Contains(t, err.Error(), "jpeg")
}

// TestCompileMarkdownMermaidMacroEscapesAndIgnoresTitle: the body is CDATA, so
// an embedded "]]>" is split across two sections (invariant 2), and a title in
// the info string is not used -- the macro carries no caption.
func TestCompileMarkdownMermaidMacroEscapesAndIgnoresTitle(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\n```mermaid title My Diagram\nflowchart TD\nA[\"x ]]> y\"]\n```\n")

	actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
		Features:      []string{"mermaid"},
		MermaidOutput: "macro",
	})
	require.NoError(t, err)

	assert.NotContains(t, actual, "My Diagram", "the macro has no caption parameter")
	assert.Contains(t, actual, `<![CDATA[flowchart TD
A["x ]]><![CDATA[]]]]><![CDATA[> y"]]]>`, "the body's ]]> is split across CDATA sections")
}

// TestCompileMarkdownMermaidMacroName covers the configurable macro key: with
// MermaidMacroName the ac:name is the one the caller named, escaped like any
// attribute value, and an empty name falls back to the default.
func TestCompileMarkdownMermaidMacroName(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\n```mermaid\nflowchart TD\nA[Start] --> B[End]\n```\n")

	t.Run("a named macro", func(t *testing.T) {
		actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
			Features:         []string{"mermaid"},
			MermaidOutput:    "macro",
			MermaidMacroName: "mermaid-diagrams",
		})
		require.NoError(t, err)

		assert.Contains(t, actual, `<ac:structured-macro ac:name="mermaid-diagrams">`)
	})

	t.Run("an empty name falls back to the default", func(t *testing.T) {
		actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
			Features:      []string{"mermaid"},
			MermaidOutput: "macro",
		})
		require.NoError(t, err)

		assert.Contains(t, actual, `<ac:structured-macro ac:name="mermaid-macro">`)
	})

	t.Run("the name is escaped", func(t *testing.T) {
		actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
			Features:         []string{"mermaid"},
			MermaidOutput:    "macro",
			MermaidMacroName: "mermaid&co",
		})
		require.NoError(t, err)

		assert.Contains(t, actual, `ac:name="mermaid&amp;co"`)
	})

	t.Run("a padded name is trimmed", func(t *testing.T) {
		actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
			Features:         []string{"mermaid"},
			MermaidOutput:    "macro",
			MermaidMacroName: "  mermaid-diagrams  ",
		})
		require.NoError(t, err)

		assert.Contains(t, actual, `<ac:structured-macro ac:name="mermaid-diagrams">`)
	})
}
