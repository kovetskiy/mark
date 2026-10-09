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
