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

func TestComplexNestedIncludeMacroIncludeMetadata(t *testing.T) {
	tempDir := t.TempDir()

	// Chain: Main Document -> inc1.md -> inc2.md -> Macro -> inc3.md (with metadata)

	// inc3.md: Included by macro expansion, contains metadata and body
	inc3Path := filepath.Join(tempDir, "inc3.md")
	inc3Content := []byte("<!-- Title: Complex Deep Title -->\n<!-- Space: DEEPSPACE -->\n# Deep Header {{ .text }}")
	require.NoError(t, os.WriteFile(inc3Path, inc3Content, 0644))

	// inc2.md: Defines a macro and invokes it. The macro produces an include for inc3.md
	inc2Path := filepath.Join(tempDir, "inc2.md")
	inc2Content := []byte(`<!-- Macro: :deep-footer:(?P<text>\w+):
Template: #inline
inline: "<!-- Include: inc3.md\ntext: ${1} -->" -->

:deep-footer:NestedResult:`)
	require.NoError(t, os.WriteFile(inc2Path, inc2Content, 0644))

	// inc1.md: Includes inc2.md
	inc1Path := filepath.Join(tempDir, "inc1.md")
	inc1Content := []byte("<!-- Include: inc2.md -->")
	require.NoError(t, os.WriteFile(inc1Path, inc1Content, 0644))

	// Main document: Includes inc1.md
	markdownInput := []byte("<!-- Include: inc1.md -->\n\nRoot document body.")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	cfg := types.MarkConfig{
		IncludePath: tempDir,
	}

	htmlOutput, _, err := CompileMarkdown(markdownInput, std, tempDir, cfg)
	require.NoError(t, err)

	// Assert that multi-level nested includes and macro expansions succeeded
	assert.Contains(t, htmlOutput, "Deep Header NestedResult")
	assert.Contains(t, htmlOutput, "Root document body")

	// Verify Metadata extracted from deep nested template
	meta, _, err := metadata.ExtractMeta(
		[]byte(htmlOutput),
		"DEEPSPACE",
		true,
		false,
		"test.md",
		nil,
		false,
		"",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, "DEEPSPACE", meta.Space)
}

func TestCircularIncludeLoopError(t *testing.T) {
	tempDir := t.TempDir()

	// a.md includes b.md
	aPath := filepath.Join(tempDir, "a.md")
	require.NoError(t, os.WriteFile(aPath, []byte("<!-- Include: b.md -->"), 0644))

	// b.md includes a.md
	bPath := filepath.Join(tempDir, "b.md")
	require.NoError(t, os.WriteFile(bPath, []byte("<!-- Include: a.md -->"), 0644))

	markdownInput := []byte("<!-- Include: a.md -->")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	cfg := types.MarkConfig{
		IncludePath: tempDir,
	}

	_, _, err = CompileMarkdown(markdownInput, std, tempDir, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular include detected")
}
