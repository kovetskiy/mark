package markdown_test

import (
	"os"
	"path/filepath"
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

// TestFormatMentionRefusesADirective: the Include directive is expanded from
// the raw text before goldmark sees a mention, so a name holding one would
// pull a file into the page.
func TestFormatMentionRefusesADirective(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.md"), []byte("TOPSECRET\n"), 0o600))

	lib, err := stdlib.New(t.Context(), nil)
	require.NoError(t, err)

	name := "<!-- Include: secret.md -->"
	actual, _, err := markdown.CompileMarkdown(t.Context(), []byte("Hi @{"+name+"}\n"), lib, filepath.Join(dir, "test.md"), markdown.Options{
		Features: []string{"mention"},
	})
	require.NoError(t, err)
	require.Contains(t, actual, "TOPSECRET", "the directive is no longer expanded inside a mention; FormatMention may accept it again")

	_, err = parser.FormatMention(name)
	assert.ErrorIs(t, err, parser.ErrUnrepresentable)
}
