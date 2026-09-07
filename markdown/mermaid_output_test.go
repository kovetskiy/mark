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
