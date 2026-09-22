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

// TestCompileMarkdownPublishesAMermaidMacro covers the third output the
// mermaid feature knows: with --mermaid-output=macro the diagram's source is
// published as a mermaid-macro macro for the instance's own Mermaid macro to
// draw, instead of an image rendered by mark.
func TestCompileMarkdownPublishesAMermaidMacro(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\nA simple Mermaid diagram:\n\n```mermaid\nflowchart TD\nA[Start] --> B[End]\n```\n")

	expected := `<h1 id="Mermaid-Test">Mermaid Test</h1>
<p>A simple Mermaid diagram:</p>
<ac:structured-macro ac:name="mermaid-macro"><ac:plain-text-body><![CDATA[flowchart TD
A[Start] --> B[End]]]></ac:plain-text-body></ac:structured-macro>
`

	actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
		Features:      []string{"mermaid"},
		MermaidOutput: "macro",
		MermaidScale:  1,
	})
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSuffix(expected, "\n"), strings.TrimSuffix(actual, "\n"))
}

// TestCompileMarkdownMermaidMacroDropsH1 checks that --drop-h1 applies to a
// mermaid-macro page like to any other.
func TestCompileMarkdownMermaidMacroDropsH1(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\nA simple Mermaid diagram:\n\n```mermaid\nflowchart TD\nA[Start] --> B[End]\n```\n")

	expected := `<p>A simple Mermaid diagram:</p>
<ac:structured-macro ac:name="mermaid-macro"><ac:plain-text-body><![CDATA[flowchart TD
A[Start] --> B[End]]]></ac:plain-text-body></ac:structured-macro>
`

	actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
		Features:      []string{"mermaid"},
		MermaidOutput: "macro",
		MermaidScale:  1,
		DropFirstH1:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSuffix(expected, "\n"), strings.TrimSuffix(actual, "\n"))
}

// TestCompileMarkdownMermaidMacroNeedsTheFeature checks that the macro output
// only applies with the mermaid feature on: without it, a mermaid fence is an
// ordinary code block regardless of --mermaid-output, like any other feature.
func TestCompileMarkdownMermaidMacroNeedsTheFeature(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	document := []byte("# Mermaid Test\n\nA simple Mermaid diagram:\n\n```mermaid\nflowchart TD\nA[Start] --> B[End]\n```\n")

	expected := `<h1 id="Mermaid-Test">Mermaid Test</h1>
<p>A simple Mermaid diagram:</p>
<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">mermaid</ac:parameter><ac:parameter ac:name="collapse">false</ac:parameter><ac:plain-text-body><![CDATA[flowchart TD
A[Start] --> B[End]]]></ac:plain-text-body></ac:structured-macro>
`

	actual, _, err := CompileMarkdown(document, lib, "doc.md", types.MarkConfig{
		Features:      []string{"mention"},
		MermaidOutput: "macro",
		MermaidScale:  1,
	})
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSuffix(expected, "\n"), strings.TrimSuffix(actual, "\n"))
}
