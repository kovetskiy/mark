package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/kovetskiy/mark/v17/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetailsSurviveTheLegacyPath covers <details> on CompileMarkdownLegacy.
// That path runs the same DetailsTransformer as the default one, which leaves
// its expand macro on a Text node as replacement-content -- but the legacy
// path had a text renderer of its own that never read the attribute, so it
// rendered the empty node the transformer had put in the element's place and
// the whole <details> block, summary and body, vanished from the page.
func TestDetailsSurviveTheLegacyPath(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
	}{
		{"block", "<details>\n<summary>Click to expand</summary>\n<p>Some hidden text</p>\n</details>\n"},
		{"inline", "- item <details><summary>Click to expand</summary>Some hidden text</details>\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy, _, err := CompileMarkdownLegacy([]byte(tt.input), lib, "testdata/test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, legacy, `<ac:structured-macro ac:name="expand">`)
			assert.Contains(t, legacy, `<ac:parameter ac:name="title">Click to expand</ac:parameter>`)
			assert.Contains(t, legacy, "Some hidden text")
			assert.NotContains(t, legacy, "<details>")

			current, _, err := CompileMarkdown([]byte(tt.input), lib, "testdata/test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Equal(t, current, legacy, "neither path has anything of its own to add to a <details> block")
		})
	}
}
