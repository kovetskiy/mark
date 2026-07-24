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

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(pipeline, 10),
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

func TestDeeplyNestedIncludeMacroIncludePipeline(t *testing.T) {
	tempDir := t.TempDir()

	// inc3.md: Included by the macro expansion
	inc3Path := filepath.Join(tempDir, "inc3.md")
	require.NoError(t, os.WriteFile(inc3Path, []byte("<!-- Title: Deep Title -->\n<!-- Space: DEEPSPACE -->\n# Deep Header {{ .text }}"), 0644))

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
	markdownInput := []byte("<!-- Include: inc1.md -->")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	macroTransformer := NewMacroTransformer(tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer(tempDir, "", std.Templates)

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(pipeline, 10),
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
	assert.Contains(t, output, "Deep Header NestedResult")
}

func TestCircularIncludeLoopErrorPipeline(t *testing.T) {
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

	macroTransformer := NewMacroTransformer(tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer(tempDir, "", std.Templates)

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(pipeline, 10),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	_ = gm.Convert(markdownInput, &buf)

	require.Error(t, pipeline.GetError())
	assert.Contains(t, pipeline.GetError().Error(), "circular include detected")
}
