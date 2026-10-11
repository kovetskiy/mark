package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinkTextIncludesStringNodes: the default path turns an <img> into a
// String node holding its alt text, and a link's text includes it, as goldmark
// v1's Node.Text did: for a bare ac: link that text names the page, and it is
// the link body. NodeText once skipped String nodes, leaving both empty. The
// legacy path has no <img> transformer and keeps the raw markup as the text.
// Both expectations are what the v1 code produced for the same input.
func TestLinkTextIncludesStringNodes(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	for _, tc := range []struct {
		name, input, want, legacy string
	}{
		{
			name:   "bare ac: link",
			input:  `[<img src="http://x/p.png" alt="Page">](ac:)` + "\n",
			want:   `<p><ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[Page]]></ac:plain-text-link-body></ac:link></p>` + "\n",
			legacy: `<p><ac:link><ri:page ri:content-title="&lt;img src=&#34;http://x/p.png&#34; alt=&#34;Page&#34; /&gt;"/><ac:plain-text-link-body><![CDATA[<img src="http://x/p.png" alt="Page" />]]></ac:plain-text-link-body></ac:link></p>` + "\n",
		},
		{
			name:   "ac: link to a named page",
			input:  `[<img src="http://x/p.png" alt="Logo">](ac:Page)` + "\n",
			want:   `<p><ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[Logo]]></ac:plain-text-link-body></ac:link></p>` + "\n",
			legacy: `<p><ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[<img src="http://x/p.png" alt="Logo" />]]></ac:plain-text-link-body></ac:link></p>` + "\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := CompileMarkdown([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)

			out, _, err = CompileMarkdownLegacy([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Equal(t, tc.legacy, out)
		})
	}
}

// TestLinkTextKeepsV1LineBreaks pins a link label broken across lines to what
// goldmark v1's Node.Text gave: a newline where a soft line break was, nothing
// where a hard one was, and a code span's own newlines kept as they were
// written, where goldmark v2's code span value turns them into spaces. Every
// expectation is what the v1 code produced for the same input, on both paths.
func TestLinkTextKeepsV1LineBreaks(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	link := func(title, body string) string {
		return `<ri:page ri:content-title="` + title + `"/><ac:plain-text-link-body><![CDATA[` + body + `]]></ac:plain-text-link-body>`
	}

	for _, tc := range []struct {
		name, input, want string
	}{
		{"soft break", "[Release\nNotes](ac:Page)\n", link("Page", "Release\nNotes")},
		{"soft break names the page", "[Release\nNotes](ac:)\n", link("Release\nNotes", "Release\nNotes")},
		{"hard break with spaces", "[Release  \nNotes](ac:Page)\n", link("Page", "ReleaseNotes")},
		{"hard break with a backslash", "[Release\\\nNotes](ac:Page)\n", link("Page", "ReleaseNotes")},
		{"soft break in emphasis", "[*Rel\nease* Notes](ac:Page)\n", link("Page", "Rel\nease Notes")},
		{"code span", "[`a\nb` c](ac:Page)\n", link("Page", "a\nb c")},
		{"code span names the page", "[`a\nb`](ac:)\n", link("a\nb", "a\nb")},
		{"code span and soft break", "[x `a\nb\nc`\ny](ac:Page)\n", link("Page", "x a\nb\nc\ny")},
		{"code span ending in a newline", "[`a\n` c](ac:Page)\n", link("Page", "a\n c")},
		{"code span trimmed at both ends", "[` a\nb `](ac:)\n", link("a\nb", "a\nb")},
		{"code span trimmed of newlines", "[`\na\n`](ac:)\n", link("a", "a")},
		{"code span with one space", "[` a`](ac:)\n", link(" a", " a")},
		{"blank code span", "[`  `](ac:)\n", link("  ", "  ")},
		{"code span in a list item", "- [`a\n  b` c\n  d](ac:)\n", link("a\nb c\nd", "a\nb c\nd")},
		{"code span in a blockquote", "> [`a\n> b`](ac:)\n", link("a\nb", "a\nb")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := CompileMarkdown([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)

			out, _, err = CompileMarkdownLegacy([]byte(tc.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
		})
	}
}
