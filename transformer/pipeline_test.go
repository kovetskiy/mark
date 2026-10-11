package transformer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/internal/goldmarktest"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
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

	macroTransformer := NewMacroTransformer("test.md", tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer("test.md", tempDir, "", std.Templates)

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](pipeline, 10),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(NewHTMLRenderer()),
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

	macroTransformer := NewMacroTransformer("test.md", tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer("test.md", tempDir, "", std.Templates)

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](pipeline, 10),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(NewHTMLRenderer()),
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

	macroTransformer := NewMacroTransformer("test.md", tempDir, "", std.Templates)
	includeTransformer := NewIncludeTransformer("test.md", tempDir, "", std.Templates)

	pipeline := NewPipelineTransformer(macroTransformer, includeTransformer)

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](pipeline, 10),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(NewHTMLRenderer()),
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	_ = gm.Convert(markdownInput, &buf)

	require.Error(t, pipeline.GetError())
	assert.Contains(t, pipeline.GetError().Error(), "circular include detected")
}

// TestIncludedDetailsBecomeAnExpandMacro: the include transformer hands its
// output on as String nodes, and a <details> block arriving that way must
// still be turned into an expand macro, not left as raw <details> markup.
func TestIncludedDetailsBecomeAnExpandMacro(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "inc.md"),
		[]byte("<details>\n<summary>S</summary>\n\nbody\n\n</details>\n"), 0o644))

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	source := []byte("<!-- Include: inc.md -->\n")
	doc := parser.New(parser.WithASTTransformers(
		util.Prioritized[parser.ASTTransformer](
			NewPipelineTransformer(NewIncludeTransformer("test.md", tempDir, "", std.Templates)), 10),
		util.Prioritized[parser.ASTTransformer](NewDetailsTransformer(), 110),
	)).Parse(source)

	var replacements, leftover []string
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if v, ok := AttributeText(node, ReplacementContentAttribute, source); ok {
			replacements = append(replacements, v)
		}
		if raw := string(ExtractNodeRawContent(node, source)); strings.Contains(raw, "<details") {
			leftover = append(leftover, raw)
		}
		return ast.WalkContinue, nil
	})

	assert.NotEmpty(t, replacements, "the details block was not rewritten")
	assert.Contains(t, strings.Join(replacements, ""), `ac:name="expand"`)
	assert.Empty(t, leftover, "raw <details> left in the document")
}
