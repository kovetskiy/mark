package transformer

import (
	"bytes"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

func TestMacroTransformer(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	markdownInput := []byte(`<!-- Macro: MYJIRA-\d+
Template: ac:jira:ticket
Ticket: ${0} -->

See task MYJIRA-123.`)

	transformer := NewMacroTransformer(".", "", std.Templates)

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
	err = gm.Convert(markdownInput, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `jira`)
	assert.Contains(t, output, `MYJIRA-123`)
	assert.NotContains(t, output, `Macro:`)
}
