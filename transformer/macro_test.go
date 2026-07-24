package transformer

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

func TestMacroTransformerInline(t *testing.T) {
	markdownInput := []byte(`<!-- Macro: :hello:(?P<name>\w+):
Template: #inline
inline: "Hello ${1}!" -->

:hello:World:`)

	transformer := NewMacroTransformer("test.md", "", "", template.New("test"))

	gm := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(transformer, 100),
			),
		),
		goldmark.WithRendererOptions(
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
