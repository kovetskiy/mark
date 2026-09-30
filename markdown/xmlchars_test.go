package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// XML 1.0 has no spelling at all for most control characters, not even a
// numeric reference, and Confluence refuses a whole page that contains one. An
// ANSI colour code in pasted terminal output, or a form feed, reached the page
// through every writer but none of the escaping, and failed it with "illegal
// character code U+001B".
func TestControlCharactersDoNotBreakThePage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		markdown string
	}{
		{"fenced code", "```\n\x1b[31mred\x1b[0m\n```\n"},
		{"fenced code title", "```bash title a\x1bb\necho\n```\n"},
		{"indented code", "    \x1b[31mred\x1b[0m\n"},
		{"inline code", "run `\x1b[31mred\x1b[0m` now\n"},
		{"form feed in text", "page one\fpage two\n"},
		{"vertical tab and NUL", "a\vb\x00c\n"},
		{"decimal reference in text", "bell &#7; and escape &#27;\n"},
		{"hex reference in text", "escape &#x1b; and &#X1B;\n"},
		{"reference in raw HTML", "<span title=\"&#x1b;\">x&#1;y</span>\n"},
		{"reference to a noncharacter", "a &#xFFFE; b <b>&#65535;</b>\n"},
		{"reference out of range", "<div>&#99999999999999999999;</div>\n"},
		{"link title", "[a](https://example.com \"t\x01t\")\n"},
		{"link text", "[a\x02b](https://example.com)\n"},
		{"page link", "[a\x02b](ac:Some\x03Page)\n"},
		{"image title and alt", "![a\x04b](https://example.com/x.png \"t\x05\")\n"},
		{"heading", "# Title\x06\n"},
		{"table cell", "| a |\n| - |\n| b\x07 |\n"},
		{"raw HTML", "<div>\x1b[1mbold</div>\n"},
		{"invalid UTF-8", "a\xed\xa0\x80b\n"},
	} {
		for name, compile := range compilers {
			t.Run(name+" "+tc.name, func(t *testing.T) {
				std, err := stdlib.New(nil)
				require.NoError(t, err)

				out, _, err := compile([]byte(tc.markdown), std, "test.md", types.MarkConfig{})
				require.NoError(t, err)

				require.NoError(t, CheckWellFormed(out), out)
				assert.Contains(t, out, "�", "the character is replaced rather than dropped")
			})
		}
	}
}

// A reference inside CDATA or a comment is text, not a character, and is
// legal as it stands; a code sample showing "&#1;" keeps it.
func TestControlCharacterReferencesAreLeftInCDATAAndComments(t *testing.T) {
	for name, compile := range compilers {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile([]byte("```\necho '&#1;'\n```\n\n<!-- &#27; -->\n"), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			require.NoError(t, CheckWellFormed(out), out)
			assert.Contains(t, out, "echo '&#1;'")
			assert.Contains(t, out, "<!-- &#27; -->")
			assert.NotContains(t, out, "�")
		})
	}
}

func TestSanitizeXMLChars(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		replaced int
	}{
		{"plain\ttext\r\n", "plain\ttext\r\n", 0},
		{"emoji \U0001F600 and �", "emoji \U0001F600 and �", 0},
		{"a\x1bb\x7fc", "a�b\x7fc", 1},
		{"￾￿", "��", 2},
		{"&#10;&#x9;&#xD;&#x1F600;&amp;&#65;", "&#10;&#x9;&#xD;&#x1F600;&amp;&#65;", 0},
		{"&#0;&#x0;&#xD800;&#xFFFF;&#x110000;", "�����", 5},
		{"&#; &#x; &#abc; &#12", "&#; &#x; &#abc; &#12", 0},
		{"<![CDATA[&#1;]]>&#1;<!--&#1;-->", "<![CDATA[&#1;]]>�<!--&#1;-->", 1},
		{"<![CDATA[&#1;", "<![CDATA[&#1;", 0},
	} {
		got, replaced := sanitizeXMLChars(tc.in)
		assert.Equal(t, tc.want, got, "%q", tc.in)
		assert.Equal(t, tc.replaced, replaced, "%q", tc.in)
	}

	// A body with nothing to replace is returned as it is.
	body := strings.Repeat("<p>text &amp; more</p>\n", 3)
	got, replaced := sanitizeXMLChars(body)
	assert.Equal(t, body, got)
	assert.Zero(t, replaced)
}
