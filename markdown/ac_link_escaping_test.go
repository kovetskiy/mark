package mark

import (
	"encoding/xml"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wellFormed reports whether body parses as XML once the ac:/ri: prefixes are
// bound. Confluence storage format must be well-formed XHTML, so a body that
// fails here is rejected rather than published.
func wellFormed(t *testing.T, body string) bool {
	t.Helper()
	return xml.Unmarshal([]byte(`<r xmlns:ac="a" xmlns:ri="r">`+body+`</r>`), new(struct{})) == nil
}

// renderLink built its ri:content-title attribute and CDATA body with raw
// writer.Write calls rather than going through a stdlib template, so it was the
// one ac:* emitter that escaped neither. Document content could make the body
// malformed or inject further attributes.
func TestACLinkEscaping(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ampersand in page title",
			input: "See the [Q&A page](ac:Q&A).\n",
			want:  `ri:content-title="Q&amp;A"`,
		},
		{
			name:  "quote in page title",
			input: "[t](<ac:Say \"Hi\">)\n",
			want:  `ri:content-title="Say &#34;Hi&#34;"`,
		},
		{
			name:  "angle brackets in page title",
			input: "[t](ac:A<b>)\n",
			want:  `ri:content-title="A&lt;b&gt;"`,
		},
		{
			name:  "CDATA terminator in link text",
			input: "[the `]]>` marker](ac:Page)\n",
			want:  `<![CDATA[the ]]><![CDATA[]]]]><![CDATA[>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown([]byte(tt.input), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, tt.want)
			assert.True(t, wellFormed(t, out), "output must be well-formed XML:\n%s", out)
		})
	}
}

// An ordinary title must be unaffected by the escaping.
func TestACLinkPlainTitleUnchanged(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte("[t](ac:MyPage)\n"), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, out, `ri:content-title="MyPage"`)
	assert.NotContains(t, out, "&#")
}
