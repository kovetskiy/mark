package transformer_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/kovetskiy/mark/v17/internal/goldmarktest"
	ctransformer "github.com/kovetskiy/mark/v17/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// convertDirectives renders Markdown through the include and macro
// transformers alone, as the pipeline runs them, with dir as the document's
// directory.
func convertDirectives(t *testing.T, dir, markdown string) (string, error) {
	t.Helper()

	pipeline := ctransformer.NewPipelineTransformer(
		ctransformer.NewIncludeTransformer("test.md", dir, "", template.New("test")),
		ctransformer.NewMacroTransformer("test.md", dir, "", template.New("test")),
	)

	gm := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized[parser.ASTTransformer](pipeline, 100)),
		),
		goldmarktest.WithRendererOptions(
			html.WithExtensions(ctransformer.NewHTMLRenderer()),
			html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	require.NoError(t, gm.Convert([]byte(markdown), &buf))

	return buf.String(), pipeline.GetError()
}

// A "<" and the rest of a comment that stand in separate nodes are text: no
// comment is written there, so no directive is read from them, whatever
// stands between the two.
func TestDirectiveInPiecesIsText(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("from the file"), 0o600))

	for name, markdown := range map[string]string{
		"include over a line break":   "Notes <\n!-- Include: outside.txt -->\n",
		"include after an empty link": "<[](https://example.com)!-- Include: outside.txt -->\n",
		"include in a link":           "<[!-- Include: outside.txt -->]()\n",
		"include in emphasis":         "<*!-- Include: outside.txt -->*\n",
		"include over two paragraphs": "- <\n\n  !-- Include: outside.txt -->\n",
		"include of a missing file":   "Notes <\n!-- Include: missing.txt -->\n",
		"include where a macro applies": "<!-- Macro: Notes\nTemplate: #x\nx: Remarks -->\n\n" +
			"Notes <\n!-- Include: outside.txt -->\n",
		"macro over blocks": "<\n\n!-- Macro: MARKER|\n\n<i>\n\nTemplate: #x\n\n<i>\n\n" +
			"\"x<i>\": from the file -->\n\nMARKER\n",
	} {
		t.Run(name, func(t *testing.T) {
			output, err := convertDirectives(t, dir, markdown)
			require.NoError(t, err)

			assert.NotContains(t, output, "from the file --&gt;</p>\n<p>from the file")
			assert.NotContains(t, output, "<p>from the file")
			assert.NotContains(t, output, "s from the file")
			assert.Contains(t, output, "--&gt;", "the text is still on the page")
			assert.Contains(t, output, "&lt;")
		})
	}
}

// A comment is one node wherever it is written, and is still a directive
// there: on lines of its own, inside a paragraph, in other markup, and in a
// fragment an include brought in.
func TestDirectiveInOneNode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("from the file"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "two.txt"), []byte("first\n\nsecond"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "heading.txt"), []byte("# Title\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested.txt"), []byte("<!-- Include: outside.txt -->\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "defines.txt"),
		[]byte("<!-- Macro: MARKER\nTemplate: #x\nx: from the macro -->\n"), 0o600))

	for name, test := range map[string]struct {
		markdown string
		want     string
	}{
		"block": {
			"<!-- Include: outside.txt -->\n\nafter\n",
			"<p>from the file</p>\n<p>after</p>\n",
		},
		"inside a paragraph": {
			"before <!-- Include: outside.txt --> after\n",
			"<p>before from the file after</p>\n",
		},
		"inside emphasis": {
			"**before <!-- Include: outside.txt -->**\n",
			"<p><strong>before from the file</strong></p>\n",
		},
		"two paragraphs inside a paragraph": {
			"before <!-- Include: two.txt --> after\n",
			"<p>before first\nsecond after</p>\n",
		},
		"a heading inside a paragraph": {
			"before <!-- Include: heading.txt --> after\n",
			"<p>before <h1>Title</h1>\n after</p>\n",
		},
		"in other markup": {
			"<div>\n<!-- Include: outside.txt -->\n</div>\n",
			"<div>\nfrom the file\n</div>\n",
		},
		"next to pieces that are text": {
			"< !-- Include: outside.txt --> <\n!-- Include: outside.txt --> <!-- Include: outside.txt -->\n",
			"<p>&lt; !-- Include: outside.txt --&gt; &lt;\n!-- Include: outside.txt --&gt; from the file</p>\n",
		},
		"in an included fragment": {
			"<!-- Include: nested.txt -->\n",
			"<p>from the file</p>\n",
		},
		"macro": {
			"<!-- Macro: MARKER\nTemplate: #x\nx: from the macro -->\n\nMARKER\n",
			"<p>from the macro</p>\n",
		},
		"macro inside a paragraph": {
			"before <!-- Macro: MARKER\nTemplate: #x\nx: from the macro --> after\n\nMARKER\n",
			"<p>before  after</p>\n<p>from the macro</p>\n",
		},
		"macro in an included fragment": {
			"<!-- Include: defines.txt -->\n\nMARKER\n",
			"<p>from the macro</p>\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := convertDirectives(t, dir, test.markdown)
			require.NoError(t, err)
			assert.Equal(t, test.want, output)
		})
	}
}

// A macro is applied to each piece of text by itself, so the markup and the
// line breaks between the pieces stay as they were written.
func TestMacroAppliesToEachPieceOfText(t *testing.T) {
	const define = "<!-- Macro: MARKER\nTemplate: #x\nx: %s -->\n\n"

	for name, test := range map[string]struct {
		expansion string
		markdown  string
		want      string
	}{
		"next to markup": {
			"replaced",
			"MARKER *a*\nb [l](https://example.com) `MARKER` MARKER\n",
			"<p>replaced <em>a</em>\nb <a href=\"https://example.com\">l</a> <code>MARKER</code> replaced</p>\n",
		},
		"before a line break": {
			"replaced",
			"a MARKER\nb MARKER  \nc\n",
			"<p>a replaced\nb replaced<br>\nc</p>\n",
		},
		"inside emphasis": {
			"replaced",
			"a **MARKER** b\n",
			"<p>a <strong>replaced</strong> b</p>\n",
		},
		"a paragraph of its own": {
			"\"<div>replaced</div>\"",
			"before\n\nMARKER\n\nafter\n",
			"<p>before</p>\n<div>replaced</div><p>after</p>\n",
		},
		"in a list item": {
			"replaced",
			"- a MARKER\n- b\n",
			"<ul>\n<li>a replaced</li>\n<li>b</li>\n</ul>\n",
		},
		"in an included fragment": {
			"replaced",
			"<!-- Include: uses.txt -->\n",
			"<p>a replaced b</p>\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "uses.txt"), []byte("a MARKER b\n"), 0o600))

			output, err := convertDirectives(t, dir, fmt.Sprintf(define, test.expansion)+test.markdown)
			require.NoError(t, err)
			assert.Equal(t, test.want, output)
		})
	}
}
