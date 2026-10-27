package mark

import (
	"strings"
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

// TestCompileMarkdownMermaidMacro covers the third output the mermaid feature
// knows: with MermaidOutput "macro" the diagram's source is published as a
// plain-text macro for the instance's own Mermaid macro to draw, instead of an
// image rendered by mark.
func TestCompileMarkdownMermaidMacro(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\nA simple Mermaid diagram:\n\n```mermaid\nflowchart TD\nA[Start] --> B[End]\n```\n")

	const (
		heading = `<h1 id="Mermaid-Test">Mermaid Test</h1>
<p>A simple Mermaid diagram:</p>
`
		paragraph = `<p>A simple Mermaid diagram:</p>
`
		macro = `<ac:structured-macro ac:name="mermaid-macro"><ac:plain-text-body><![CDATA[flowchart TD
A[Start] --> B[End]]]></ac:plain-text-body></ac:structured-macro>
`
		codeBlock = `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">mermaid</ac:parameter><ac:parameter ac:name="collapse">false</ac:parameter><ac:plain-text-body><![CDATA[flowchart TD
A[Start] --> B[End]]]></ac:plain-text-body></ac:structured-macro>
`
	)

	tests := []struct {
		name   string
		cfg    types.MarkConfig
		expect string
	}{
		{
			name:   "publishes the source as the macro",
			cfg:    types.MarkConfig{Features: []string{"mermaid"}, MermaidOutput: "macro"},
			expect: heading + macro,
		},
		{
			name:   "drop-h1 applies like to any page",
			cfg:    types.MarkConfig{Features: []string{"mermaid"}, MermaidOutput: "macro", DropFirstH1: true},
			expect: paragraph + macro,
		},
		{
			name:   "needs the feature to apply",
			cfg:    types.MarkConfig{Features: []string{"mention"}, MermaidOutput: "macro"},
			expect: heading + codeBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, _, err := CompileMarkdown(document, lib, "doc.md", tt.cfg)
			require.NoError(t, err)

			assert.Equal(t, strings.TrimSuffix(tt.expect, "\n"), strings.TrimSuffix(actual, "\n"))
		})
	}
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
