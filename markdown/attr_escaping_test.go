package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileEsc(t *testing.T, src string, features ...string) string {
	t.Helper()
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{
		Features: features,
	})
	require.NoError(t, err)
	return out
}

// ac:title was interpolated raw while the adjacent ac:alt was escaped via
// nodeToHTMLText, so a quote in a markdown image title closed the attribute
// early and produced malformed XML that Confluence rejects.
func TestImageTitleAttributeIsEscaped(t *testing.T) {
	out := compileEsc(t, "![alt](https://x/a.png 'has \" quote')\n")

	assert.Contains(t, out, `ac:title="has &#34; quote"`)
	assert.NotContains(t, out, `ac:title="has " quote"`,
		"an unescaped quote closes the attribute early")
}

// The alt text is already escaped upstream by nodeToHTMLText; escaping it again
// in the template would double-encode it.
func TestImageAltAttributeIsNotDoubleEscaped(t *testing.T) {
	out := compileEsc(t, "![a & b](https://x/a.png)\n")

	assert.Contains(t, out, `ac:alt="a &amp; b"`)
	assert.NotContains(t, out, "&amp;amp;", "alt must not be escaped twice")
}

// The date value came straight from the document into the datetime attribute, so
// a quote in it let the rest be read as further attributes.
func TestDateAttributeIsEscaped(t *testing.T) {
	out := compileEsc(t, `@date(2026" onx="y)`+"\n", "date")

	assert.NotContains(t, out, `onx="y"`, "attribute injection must not be possible")
	assert.Contains(t, out, "&#34;")
}

// A well-formed date must be unchanged by the escaping.
func TestDateAttributeNormalValueUnchanged(t *testing.T) {
	out := compileEsc(t, "@date(2026-07-27)\n", "date")

	assert.Contains(t, out, `<time datetime="2026-07-27" />`)
	assert.NotContains(t, out, "&#")
}
