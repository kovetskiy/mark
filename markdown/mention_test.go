package markdown_test

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/parser"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatMentionPublishesTheName runs mentions written by
// parser.FormatMention through publishing's whole pipeline: with no
// Confluence to resolve the user, the page holds the name as text, exactly
// once, and nothing of the escaping.
func TestFormatMentionPublishesTheName(t *testing.T) {
	lib, err := stdlib.New(t.Context(), nil)
	require.NoError(t, err)

	for _, name := range []string{"Ops {on call}", `CORP\jdoe`, `a\}`, "a|b", "<b>&amp;", "*x* `y`"} {
		t.Run(name, func(t *testing.T) {
			mention, err := parser.FormatMention(name)
			require.NoError(t, err)

			for _, source := range []string{
				"Hello " + mention + "!\n",
				"| who | what |\n| --- | --- |\n| " + mention + " | x |\n",
			} {
				actual, _, err := markdown.CompileMarkdown(t.Context(), []byte(source), lib, "test.md", markdown.Options{
					Features: []string{"mention"},
				})
				require.NoError(t, err)

				assert.Equal(t, 1, strings.Count(actual, stdlib.XMLEscape(name)), "%s\n%s", source, actual)
				assert.NotContains(t, actual, "@{", actual)
				if !strings.Contains(name, `\`) {
					assert.NotContains(t, actual, `\`, actual)
				}
			}
		})
	}
}
