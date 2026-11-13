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
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// convertDirectives renders Markdown through the macro and include
// transformers alone, in the order markdown's pipeline runs them, with dir as
// the document's directory.
func convertDirectives(t *testing.T, dir, markdown string) (string, error) {
	t.Helper()

	pipeline := ctransformer.NewPipelineTransformer(
		ctransformer.NewMacroTransformer("test.md", dir, "", template.New("test")),
		ctransformer.NewIncludeTransformer("test.md", dir, "", template.New("test")),
	)

	gm := goldmarktest.New(
		goldmarktest.WithExtensions(extension.NewDefinitionListParser(), extension.NewDefinitionListHTMLRenderer()),
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
		"after markup, where it would open a list by itself": {
			"replaced",
			"**Owner** - MARKER\n\n**Step** 1. MARKER\n",
			"<p><strong>Owner</strong> - replaced</p>\n<p><strong>Step</strong> 1. replaced</p>\n",
		},
		"after markup, where it would open a heading or a quote": {
			"replaced",
			"**a** # MARKER\n\n# **b** > MARKER\n",
			"<p><strong>a</strong> # replaced</p>\n<h1><strong>b</strong> &gt; replaced</h1>\n",
		},
		"inside emphasis, where it would open a list by itself": {
			"replaced",
			"**- MARKER**\n",
			"<p><strong>- replaced</strong></p>\n",
		},
		"after a line break, where it would open a list by itself": {
			"replaced",
			"a\n2. MARKER b\n",
			"<p>a\n2. replaced b</p>\n",
		},
		"markup after markup": {
			"\"<div>replaced</div>\"",
			"**a** MARKER *b*\n",
			"<p><strong>a</strong> <div>replaced</div> <em>b</em></p>\n",
		},
		"nothing after markup": {
			"\"\"",
			"**a**MARKER*b*\n",
			"<p><strong>a</strong><em>b</em></p>\n",
		},
		"a block at the start of a paragraph": {
			"\"# replaced\"",
			"MARKER\n",
			"<h1>replaced</h1>\n",
		},
		"in an included fragment that defines it": {
			"replaced",
			"<!-- Include: uses.txt -->\n",
			"<p>a replaced b</p>\n",
		},
		"in a heading, where it would open a list by itself": {
			"replaced",
			"## 1. MARKER\n",
			"<h2>1. replaced</h2>\n",
		},
		"spaces alone between markup": {
			"\"\"",
			"**a** MARKER *b*\n",
			"<p><strong>a</strong>  <em>b</em></p>\n",
		},
		"nothing, on lines of its own": {
			"\"\"",
			"MARKER\nMARKER\n\nMARKER  \ntext\n\na\nMARKER\nb\n",
			"<p>text</p>\n<p>a\n\nb</p>\n",
		},
		"in a definition's term": {
			"replaced",
			"a MARKER\n: b\n",
			"<dl>\n<dt>a replaced</dt>\n<dd>b</dd>\n</dl>\n",
		},
		"not in an image's description": {
			"\"<b>replaced</b>\"",
			"![MARKER](https://example.com/x.png) MARKER\n",
			"<p><img src=\"https://example.com/x.png\" alt=\"MARKER\"> <b>replaced</b></p>\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "uses.txt"), []byte(fmt.Sprintf(define, test.expansion)+"a MARKER b\n"), 0o600))

			output, err := convertDirectives(t, dir, fmt.Sprintf(define, test.expansion)+test.markdown)
			require.NoError(t, err)
			assert.Equal(t, test.want, output)
		})
	}
}

// A pattern is matched against the text as it is written: a "[" or a "*"
// that opens no markup is part of the text around it.
func TestMacroPatternHoldsMarkupCharacters(t *testing.T) {
	const define = "<!-- Macro: %s\nTemplate: #x\nx: replaced -->\n\n"

	for name, test := range map[string]struct {
		pattern  string
		markdown string
		want     string
	}{
		"brackets": {
			`\[T-1\]`,
			"see [T-1] here\n",
			"<p>see replaced here</p>\n",
		},
		"brackets next to markup and line breaks": {
			`\[T-1\]`,
			"a *[T-1]* [T-1]\n[T-1]  \n[T-1] `[T-1]`\n",
			"<p>a <em>replaced</em> replaced\nreplaced<br>\nreplaced <code>[T-1]</code></p>\n",
		},
		"brackets in a list item": {
			`\[T-1\]`,
			"- a [T-1] b\n",
			"<ul>\n<li>a replaced b</li>\n</ul>\n",
		},
		"brackets in an included fragment that defines it": {
			`\[T-1\]`,
			"<!-- Include: uses.txt -->\n",
			"<p>a replaced <em>replaced</em></p>\n",
		},
		"an asterisk": {
			`A\*B`,
			"see A*B here\n",
			"<p>see replaced here</p>\n",
		},
		"a link's text is not its brackets": {
			`\[T-1\]`,
			"a [T-1](https://example.com)\n",
			"<p>a <a href=\"https://example.com\">T-1</a></p>\n",
		},
		"not over a line break": {
			"a b",
			"see a\nb here\n",
			"<p>see a\nb here</p>\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "uses.txt"), []byte(fmt.Sprintf(define, test.pattern)+"a [T-1] *[T-1]*\n"), 0o600))

			output, err := convertDirectives(t, dir, fmt.Sprintf(define, test.pattern)+test.markdown)
			require.NoError(t, err)
			assert.Equal(t, test.want, output)
		})
	}
}
