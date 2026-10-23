package transformer_test

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/kovetskiy/mark/v16/internal/goldmarktest"
	cmarkdown "github.com/kovetskiy/mark/v16/markdown"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

func TestMacroTransformerInline(t *testing.T) {
	markdownInput := []byte(`<!-- Macro: :hello:(?P<name>\w+):
Template: #inline
inline: "Hello ${1}!" -->

:hello:World:`)

	transformer := ctransformer.NewMacroTransformer("test.md", "", "", template.New("test"))

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](transformer, 100),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(ctransformer.NewHTMLRenderer()),
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	err := gm.Convert(markdownInput, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Hello World!")
	assert.NotContains(t, output, "Macro:")
}

func TestMultipleMacrosDefinition(t *testing.T) {
	markdownInput := []byte(`<!-- Macro: :hello:(?P<name>\w+):
Template: #inline
inline: "Hello ${1}!" -->

<!-- Macro: :status:(?P<status>\w+):
Template: #inline
inline: "Status is ${1}." -->

<!-- Macro: :box:(?P<text>\w+):
Template: #inline
inline: "Box: ${1}." -->

:hello:World:
:status:active:`)

	transformer := ctransformer.NewMacroTransformer("test.md", "", "", template.New("test"))

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](transformer, 100),
			),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(ctransformer.NewHTMLRenderer()),
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	err := gm.Convert(markdownInput, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Hello World!")
	assert.Contains(t, output, "Status is active.")
	assert.NotContains(t, output, "Macro:")
}

func TestMacroInTableCell(t *testing.T) {
	markdownInput := []byte(`<!-- Macro: :accept:
Template: #inline
inline: "<ac:structured-macro ac:name=\"status\"><ac:parameter ac:name=\"title\">ACCEPTED</ac:parameter></ac:structured-macro>" -->

| Status | Description |
| --- | --- |
| :accept: | Approved |`)

	output, _, err := cmarkdown.CompileMarkdown(markdownInput, nil, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, output, "<ac:structured-macro ac:name=\"status\">")
	assert.NotContains(t, output, "&lt;ac:structured-macro")
}
