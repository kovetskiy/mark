package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOption2MacroIncludeMetadataPipeline(t *testing.T) {
	tempDir := t.TempDir()

	// Create an included template file that defines Metadata and Body content
	includedTemplatePath := filepath.Join(tempDir, "meta_header.md")
	includedContent := []byte("<!-- Title: Generated Page Title -->\n<!-- Space: MYSPACE -->\n\n# Welcome to {{ .name }}")
	err := os.WriteFile(includedTemplatePath, includedContent, 0644)
	require.NoError(t, err)

	// Main Markdown content contains a Macro definition that outputs an Include directive
	markdownInput := []byte(`<!-- Macro: :gen-header:(?P<name>\w+):
Template: #inline
inline: "<!-- Include: meta_header.md\nname: ${1} -->" -->

:gen-header:World:

This is the main body text.`)

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	cfg := types.MarkConfig{
		IncludePath: tempDir,
	}

	// Compile Markdown using Goldmark AST transformers pipeline
	htmlOutput, _, err := CompileMarkdown(markdownInput, std, tempDir, cfg)
	require.NoError(t, err)

	// Assert that Macro expanded into Include, and Include expanded into Content
	assert.Contains(t, htmlOutput, "Welcome to World")
	assert.Contains(t, htmlOutput, "main body text")

	// Verify Metadata can be extracted from the expanded document
	meta, _, err := metadata.ExtractMeta(
		[]byte(htmlOutput),
		"MYSPACE",
		true,
		false,
		"test.md",
		nil,
		false,
		"",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, "MYSPACE", meta.Space)
}
