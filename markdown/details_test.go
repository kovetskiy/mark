package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertDetailsToExpand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "basic details and summary",
			input: `<details>
<summary>Click to expand</summary>
<p>Some hidden text</p>
</details>`,
			expected: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Click to expand</ac:parameter><ac:rich-text-body>
<p>Some hidden text</p>
</ac:rich-text-body></ac:structured-macro>`,
		},
		{
			name: "details without summary",
			input: `<details>
<p>Some hidden text</p>
</details>`,
			expected: `<ac:structured-macro ac:name="expand"><ac:rich-text-body>
<p>Some hidden text</p>
</ac:rich-text-body></ac:structured-macro>`,
		},
		{
			name: "summary with html tags inside",
			input: `<details>
<summary>Click <b>to</b> <i>expand</i></summary>
<p>Some hidden text</p>
</details>`,
			expected: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Click to expand</ac:parameter><ac:rich-text-body>
<p>Some hidden text</p>
</ac:rich-text-body></ac:structured-macro>`,
		},
		{
			name: "nested details",
			input: `<details>
<summary>Outer</summary>
<p>Outer content</p>
<details>
<summary>Inner</summary>
<p>Inner content</p>
</details>
</details>`,
			expected: `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Outer</ac:parameter><ac:rich-text-body><p>Outer content</p><ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Inner</ac:parameter><ac:rich-text-body><p>Inner content</p></ac:rich-text-body></ac:structured-macro></ac:rich-text-body></ac:structured-macro>`,
		},
	}

	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, _, err := CompileMarkdown([]byte(tt.input), lib, "testdata/test.md", cfg)
			assert.NoError(t, err)

			// Normalize spaces/newlines to make assertions robust
			normActual := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(actual, "\n", ""), "\r", ""), " ", "")
			normExpected := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(tt.expected, "\n", ""), "\r", ""), " ", "")
			assert.Equal(t, normExpected, normActual)
		})
	}
}

func TestDetailsWithFencedCodeBlock(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	markdown := []byte("```xml\n<![CDATA[hello world]]>\n```\n\n<details>\n<summary>Details</summary>\n<p>Some content</p>\n</details>")

	actual, _, err := CompileMarkdown(markdown, lib, "testdata/test.md", cfg)
	assert.NoError(t, err)
	assert.Contains(t, actual, "<![CDATA[hello world]]>")
	assert.NotContains(t, actual, "<!--[CDATA[")
}

// TestDetailsInCodeSpanIsNotConverted covers documentation that mentions the
// tag literally. `<details>` inside backticks is content, not markup; the
// transformer used to consume it and silently emit an empty <code></code>.
func TestDetailsInCodeSpanIsNotConverted(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	tests := []string{
		"`<details>` maps to the native expand macro.\n",
		"| `details` | `<details>` / `<summary>` |\n|---|---|\n| a | b |\n",
		"Use `<details>` and `</details>` to wrap it.\n",
	}

	for _, input := range tests {
		actual, _, err := CompileMarkdown([]byte(input), lib, "testdata/test.md", cfg)
		assert.NoError(t, err)
		assert.Contains(t, actual, "&lt;details&gt;", "literal tag should survive inside a code span")
		assert.NotContains(t, actual, "<code></code>", "code span content was swallowed")
		assert.NotContains(t, actual, `ac:name="expand"`, "code span must not produce a macro")
	}
}

// TestDetailsWithBlankLines covers <details> bodies containing blank lines. A
// blank line ends the HTML block, so the element is split across sibling AST
// nodes and each fragment alone is unbalanced. html.Parse used to auto-close
// the dangling tag, which stranded the body outside the macro and emitted a
// literal </details> that Confluence rejects with
// "Error parsing xhtml: Unexpected close tag </details>".
func TestDetailsWithBlankLines(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "blank lines around body",
			input: "<details>\n<summary>Click to expand</summary>\n\nSome body text.\n\n</details>\n",
		},
		{
			name:  "nested details with blank lines",
			input: "<details>\n<summary>Outer</summary>\n\nOuter body.\n\n<details>\n<summary>Inner</summary>\n\nInner body.\n\n</details>\n</details>\n",
		},
		{
			name:  "blank line separated body inside a layout cell",
			input: "<!-- ac:layout-cell -->\n\n<details>\n<summary>S</summary>\n\nBody.\n\n</details>\n\n<!-- ac:layout-cell end -->\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, _, err := CompileMarkdown([]byte(tt.input), lib, "testdata/test.md", cfg)
			assert.NoError(t, err)

			assert.NotContains(t, actual, "<details", "raw <details> tag leaked into output")
			assert.NotContains(t, actual, "</details>", "stray closing tag would break Confluence xhtml parsing")
			assert.NotContains(t, actual, "<summary", "raw <summary> tag leaked into output")
			assert.Contains(t, actual, `<ac:structured-macro ac:name="expand">`)

			assert.Equal(t,
				strings.Count(actual, "<ac:rich-text-body>"),
				strings.Count(actual, "</ac:rich-text-body>"),
				"unbalanced ac:rich-text-body")
		})
	}
}

// TestDetailsBodyMarkdownIsRendered asserts the blank-line form keeps its
// Markdown semantics: the body stays a separate AST node, so inline markup in
// it must still be converted rather than passed through literally.
func TestDetailsBodyMarkdownIsRendered(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	markdown := []byte("<details>\n<summary>S</summary>\n\nBody with **bold** text.\n\n</details>\n")

	actual, _, err := CompileMarkdown(markdown, lib, "testdata/test.md", cfg)
	assert.NoError(t, err)
	assert.Contains(t, actual, "<strong>bold</strong>")
	assert.NotContains(t, actual, "**bold**")
}

// TestDetailsMalformedStaysBalanced guards the fallback paths. Whatever the
// input, the emitted expand macros must be balanced -- Confluence rejects the
// whole page otherwise -- and a closing tag with nothing open must not be
// turned into a macro end.
func TestDetailsMalformedStaysBalanced(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	tests := []struct {
		name  string
		input string
	}{
		{"unclosed details", "<details>\n<summary>S</summary>\n\nbody\n"},
		{"unclosed nested details", "<details>\n<summary>O</summary>\n\n<details>\n<summary>I</summary>\n\nbody\n"},
		{"stray closing tag only", "text\n\n</details>\n"},
		{"two sequential details", "<details>\n<summary>A</summary>\n\na\n\n</details>\n\n<details>\n<summary>B</summary>\n\nb\n\n</details>\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, _, err := CompileMarkdown([]byte(tt.input), lib, "testdata/test.md", cfg)
			assert.NoError(t, err)
			assert.Equal(t,
				strings.Count(actual, "<ac:rich-text-body>"),
				strings.Count(actual, "</ac:rich-text-body>"),
				"unbalanced ac:rich-text-body in %q", actual)
			assert.Equal(t,
				strings.Count(actual, "<ac:structured-macro"),
				strings.Count(actual, "</ac:structured-macro>"),
				"unbalanced ac:structured-macro in %q", actual)
		})
	}
}

// TestDetailsInFencedCodeBlockIsLiteral ensures a documented example inside a
// fenced code block is never converted into a macro.
func TestDetailsInFencedCodeBlockIsLiteral(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	cfg := types.MarkConfig{}

	markdown := []byte("```html\n<details>\n<summary>S</summary>\n</details>\n```\n")

	actual, _, err := CompileMarkdown(markdown, lib, "testdata/test.md", cfg)
	assert.NoError(t, err)
	assert.Contains(t, actual, "<details>")
	assert.NotContains(t, actual, `ac:name="expand"`)
}

// TestCDATAInsideDetailsSurvivesUnaltered: converting a <details> parses the
// fragment as HTML, and html.Parse has no notion of CDATA. It reads
// "<![CDATA[" as a bogus comment ending at the first ">", and everything after
// that as markup -- so a sample containing ">" was cut in two: the tail
// re-parsed and rewritten, the section left unterminated.
//
// A code sample is the one thing that must reach the page as written, and this
// rewrote it and then failed the publish for it.
func TestCDATAInsideDetailsSurvivesUnaltered(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	sample := "<![CDATA[if a > b then <br> done]]>"
	input := "<details>\n<summary>Code</summary>\n" +
		`<ac:structured-macro ac:name="code"><ac:plain-text-body>` + sample +
		"</ac:plain-text-body></ac:structured-macro>\n</details>\n"

	actual, _, err := CompileMarkdown([]byte(input), lib, "testdata/test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, actual, sample, "the sample reaches the page as written")
	assert.NotContains(t, actual, "<br />", "and nothing inside it was rewritten")
	assert.NotContains(t, actual, "]]&gt;", "and the section is not left unterminated")

	assert.Contains(t, actual, `ac:name="expand"`, "the details still became a macro")
	require.NoError(t, CheckWellFormed(actual))
}

// TestCDATAWithoutAngleBracketStillWorks is the case that happened to survive
// before, kept so the fix is not mistaken for the whole of the behaviour.
func TestCDATAWithoutAngleBracketStillWorks(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	sample := "<![CDATA[plain sample]]>"
	input := "<details>\n<summary>Code</summary>\n" +
		`<ac:structured-macro ac:name="code"><ac:plain-text-body>` + sample +
		"</ac:plain-text-body></ac:structured-macro>\n</details>\n"

	actual, _, err := CompileMarkdown([]byte(input), lib, "testdata/test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, actual, sample)
	require.NoError(t, CheckWellFormed(actual))
}
