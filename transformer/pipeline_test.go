package transformer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

func TestMacroThenIncludeTransformerPipeline(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create included template file
	includedTemplatePath := filepath.Join(tempDir, "meta_header.md")
	includedContent := []byte("# Welcome to {{ .name }}\n\nThis is from include.")
	err := os.WriteFile(includedTemplatePath, includedContent, 0644)
	require.NoError(t, err)

	// 2. Main Markdown input containing a Macro that produces an Include directive
	markdownInput := []byte(`<!-- Macro: :gen-header:(?P<name>\w+):
Template: #inline
inline: "<!-- Include: meta_header.md\nname: ${1} -->" -->

:gen-header:World:

Main body text.`)

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	macroTransformer := NewMacroTransformer(tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer(tempDir, "", std.Templates)

	gm := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(macroTransformer, 10),
				util.Prioritized(includeTransformer, 20),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	err = gm.Convert(markdownInput, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Welcome to World")
	assert.Contains(t, output, "This is from include.")
	assert.Contains(t, output, "Main body text.")
	assert.NotContains(t, output, "Macro:")
}
