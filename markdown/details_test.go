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
