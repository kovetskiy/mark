package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTightDefinitionDescriptionKeepsItsLaterParagraphs: goldmark v1 made the
// first paragraph of a tight description a TextBlock and stopped there, so a
// second paragraph stayed a <p>. Turning every one of them into a TextBlock ran
// the paragraphs together into one, which Confluence shows as one run of text.
func TestTightDefinitionDescriptionKeepsItsLaterParagraphs(t *testing.T) {
	source := []byte("Term\n: first para\n\n    second para\n\n    third para\n")
	want := "<table>\n<tbody>\n<tr>\n<th>Term</th>\n<td>first para\n<p>second para</p>\n<p>third para</p>\n</td>\n</tr>\n</tbody>\n</table>\n"

	for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
		"default": CompileMarkdown,
		"legacy":  CompileMarkdownLegacy,
	} {
		std, err := stdlib.New(nil)
		require.NoError(t, err)

		out, _, err := compile(source, std, "test.md", types.MarkConfig{})
		require.NoError(t, err, name)
		assert.Equal(t, want, out, name)
	}
}
