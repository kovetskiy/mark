package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In an inline context goldmark emits one RawHTML node per tag, so the fragment
// holding "<details>" does not contain its own "<summary>". The summary was
// therefore never lifted into ac:parameter and leaked into the macro body as a
// literal <summary> tag — unlike a block context, which arrives as a single
// HTMLBlock and worked correctly.
func TestDetailsInlineContextsLiftSummaryToTitle(t *testing.T) {
	const wantMacro = `<ac:structured-macro ac:name="expand">` +
		`<ac:parameter ac:name="title">s</ac:parameter>` +
		`<ac:rich-text-body>x</ac:rich-text-body></ac:structured-macro>`

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "table cell",
			input: "| A |\n|---|\n| <details><summary>s</summary>x</details> |\n",
		},
		{
			name:  "list item",
			input: "- item <details><summary>s</summary>x</details>\n",
		},
		{
			name:  "blockquote",
			input: "> quote <details><summary>s</summary>x</details>\n",
		},
		{
			// The block form already worked; kept so a fix here cannot regress it.
			name:  "block context",
			input: "<details><summary>s</summary>x</details>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown([]byte(tt.input), std, "test.md", types.MarkConfig{
				Features: []string{"details"},
			})
			require.NoError(t, err)

			assert.Contains(t, out, wantMacro)
			assert.NotContains(t, out, "<summary>",
				"the summary must become ac:parameter, not body content")
			assert.NotContains(t, out, "</details>",
				"no literal details tag may survive")
		})
	}
}

// A <details> with no summary must still produce a macro (without a title) and
// must not swallow the surrounding inline content while looking for one.
func TestDetailsInlineWithoutSummary(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		[]byte("| A |\n|---|\n| before <details>body</details> after |\n"),
		std, "test.md", types.MarkConfig{Features: []string{"details"}},
	)
	require.NoError(t, err)

	assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:rich-text-body>body`)
	assert.NotContains(t, out, `ac:name="title"`)
	assert.Contains(t, out, "before ", "content before the macro must survive")
	assert.Contains(t, out, " after", "content after the macro must survive")
}
