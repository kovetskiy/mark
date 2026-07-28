package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileWithImgTag(t *testing.T, src string) string {
	t.Helper()
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{
		Features: []string{"html-img-tag"},
	})
	require.NoError(t, err)
	return out
}

// The transformer replaced nodes while ast.Walk was iterating over the sibling
// it had just detached, so the walk abandoned the rest of that parent's children
// after the first <img>. Later images stayed as raw <img>, which Confluence
// storage format cannot represent.
func TestHTMLImgTransformerConvertsEveryImage(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "two inline images in one paragraph",
			src:  `A <img src="https://x/a.png"> B <img src="https://x/b.png"> C`,
			want: 2,
		},
		{
			name: "three inline images",
			src:  `<img src="https://x/a.png"><img src="https://x/b.png"><img src="https://x/c.png">x`,
			want: 3,
		},
		{
			name: "image-only blocks separated by prose",
			src:  "<img src=\"https://x/a.png\">\n\nmid\n\n<img src=\"https://x/b.png\">\n",
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := compileWithImgTag(t, tt.src)
			assert.Equal(t, tt.want, countSubstr(out, "<ac:image>"))
			assert.NotContains(t, out, "<img ",
				"no raw <img> may survive: Confluence has no such element")
		})
	}
}

// The block path replaced the whole HTMLBlock with just its images, silently
// dropping any other markup it contained -- including the caption in the common
// centered-image idiom.
func TestHTMLImgTransformerKeepsSurroundingBlockMarkup(t *testing.T) {
	out := compileWithImgTag(t,
		"<div align=\"center\">\n<img src=\"https://x/a.png\">\n<b>caption</b>\n</div>\n")

	assert.Contains(t, out, "caption", "the caption must not be discarded")
	assert.Contains(t, out, "<div", "the wrapper must not be discarded")
}

// A block holding nothing but images (and whitespace) is still taken over, since
// replacing it loses nothing.
func TestHTMLImgTransformerStillConvertsImageOnlyBlocks(t *testing.T) {
	out := compileWithImgTag(t, "<img src=\"https://x/a.png\">\n<img src=\"https://x/b.png\">\n")

	assert.Equal(t, 2, countSubstr(out, "<ac:image>"))
	assert.NotContains(t, out, "<img ")
}

func countSubstr(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
