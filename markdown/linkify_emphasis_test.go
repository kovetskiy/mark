package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinkifyInsideEmphasis: a bare URL that is all an emphasis holds is a
// link, as it is anywhere else in a paragraph.
func TestLinkifyInsideEmphasis(t *testing.T) {
	for source, want := range map[string]string{
		"**https://x.com/a**":   `<p><strong><a href="https://x.com/a">https://x.com/a</a></strong></p>`,
		"*https://x.com/a*":     `<p><em><a href="https://x.com/a">https://x.com/a</a></em></p>`,
		"__www.x.com__":         `<p><strong><a href="http://www.x.com">www.x.com</a></strong></p>`,
		"**a@b.co**":            `<p><strong><a href="mailto:a@b.co">a@b.co</a></strong></p>`,
		"~~https://x.com~~":     `<p><del><a href="https://x.com">https://x.com</a></del></p>`,
		"***https://x.com***":   `<p><em><strong><a href="https://x.com">https://x.com</a></strong></em></p>`,
		"**see https://x.com**": `<p><strong>see <a href="https://x.com">https://x.com</a></strong></p>`,
		"**a.b** and *x.y.*":    `<p><strong>a.b</strong> and <em>x.y.</em></p>`,
	} {
		for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
			"default": CompileMarkdown,
			"legacy":  CompileMarkdownLegacy,
		} {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile([]byte(source), std, "test.md", types.MarkConfig{})
			require.NoError(t, err, "%s: %q", name, source)
			assert.Equal(t, want+"\n", out, "%s: %q", name, source)
		}
	}
}
