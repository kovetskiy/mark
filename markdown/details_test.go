package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
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
	cfg := types.MarkConfig{
		Features: []string{"details"},
	}

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
	cfg := types.MarkConfig{
		Features: []string{"details"},
	}

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
	cfg := types.MarkConfig{Features: []string{"details"}}

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
