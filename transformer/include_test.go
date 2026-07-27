package transformer_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	cmarkdown "github.com/kovetskiy/mark/v16/markdown"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

func TestIncludeTransformer(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "header.md")
	err := os.WriteFile(templatePath, []byte("# Header from Include\n\nHello {{ .name }}"), 0644)
	require.NoError(t, err)

	markdownInput := []byte("<!-- Include: header.md\nname: World -->\n\nMain content here.")

	transformer := ctransformer.NewIncludeTransformer("test.md", tempDir, "", template.New("test"))

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
	assert.Contains(t, output, "Header from Include")
	assert.Contains(t, output, "Hello World")
	assert.Contains(t, output, "Main content here.")
}

func TestIncludeTransformerUnsafeHTML(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "raw_html.md")
	err := os.WriteFile(templatePath, []byte("<div><ac:structured-macro ac:name=\"info\"><ac:rich-text-body><p>Unescaped content</p></ac:rich-text-body></ac:structured-macro></div>"), 0644)
	require.NoError(t, err)

	markdownInput := []byte("<!-- Include: raw_html.md -->")

	output, _, err := cmarkdown.CompileMarkdown(markdownInput, nil, filepath.Join(tempDir, "test.md"), types.MarkConfig{
		IncludePath: tempDir,
	})
	require.NoError(t, err)

	assert.Contains(t, output, "<ac:structured-macro ac:name=\"info\">")
	assert.NotContains(t, output, "&lt;ac:structured-macro")
}
