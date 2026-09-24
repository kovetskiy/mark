package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileWithImgTag(t *testing.T, src string) string {
	t.Helper()
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte(src), std, "test.md", types.MarkConfig{})
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

// An <img> inside a block of other markup was left as written: an HTML block
// has no RawHTML children for the inline path to find, and the block path only
// took over blocks holding nothing but images. The README idiom below published
// a relative <img /> Confluence does not render, and never uploaded the file.
func TestHTMLImgInsideMarkupBlockIsAttached(t *testing.T) {
	doc := imageDocument(t, "my file.png", "other.png")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	src := "<p align=\"center\">\n" +
		"  <img src=\"my%20file.png\" width=\"200\" alt=\"A &amp; B\" title=\"T\">\n" +
		"  <img src=\"other.png\"><br>\n" +
		"  <img src=\"https://example.com/remote.png\">\n" +
		"</p>\n"

	out, attachments, err := CompileMarkdown([]byte(src), std, doc, types.MarkConfig{})
	require.NoError(t, err)
	require.NoError(t, CheckWellFormed(out))

	assert.NotContains(t, out, "<img")
	assert.Contains(t, out, "<p align=\"center\">\n  <ac:image ", "the surrounding markup is kept as written")
	assert.Contains(t, out, `ac:width="200" ac:title="T" ac:alt="A &amp; B"><ri:attachment ri:filename="my file.png"/></ac:image>`)
	assert.Contains(t, out, `<ri:attachment ri:filename="other.png"/></ac:image><br />`)
	assert.Contains(t, out, `<ri:url ri:value="https://example.com/remote.png"/>`)
	assert.Contains(t, out, "</p>")

	var names []string
	for _, a := range attachments {
		names = append(names, a.Filename)
	}
	assert.ElementsMatch(t, []string{"my file.png", "other.png"}, names,
		"the files have to be uploaded as well as referenced")
}

// An <img> in a comment or a CDATA section is not markup, and a <picture> has
// no storage format to become, so none of them is touched -- while the <img>
// beside them in the same block still is.
func TestHTMLImgInsideMarkupBlockLeavesNonImagesAlone(t *testing.T) {
	src := "<div>\n" +
		"<!-- <img src=\"commented.png\"> -->\n" +
		"<![CDATA[ <img src=\"cdata.png\"> ]]>\n" +
		"<picture><source srcset=\"dark.png\"><img src=\"light.png\"></picture>\n" +
		"<img src=\"https://example.com/a.png\">\n" +
		"</div>\n"

	out := compileWithImgTag(t, src)
	require.NoError(t, CheckWellFormed(out))

	assert.Contains(t, out, `<!-- <img src="commented.png"> -->`)
	assert.Contains(t, out, `<![CDATA[ <img src="cdata.png"> ]]>`)
	assert.Contains(t, out, `<img src="light.png" />`)
	assert.Equal(t, 1, countSubstr(out, "<ac:image>"))
}

// The <details> transformer rewrites its block into markup before the <img>
// transformer runs; an image inside it is converted all the same.
func TestHTMLImgInsideDetailsIsConverted(t *testing.T) {
	out := compileWithImgTag(t,
		"<details>\n<summary>S</summary>\n<img src=\"https://example.com/a.png\" alt=\"a\">\n</details>\n")
	require.NoError(t, CheckWellFormed(out))

	assert.Contains(t, out, `ac:name="expand"`)
	assert.Contains(t, out, `<ac:image ac:alt="a"><ri:url ri:value="https://example.com/a.png"/></ac:image>`)
	assert.NotContains(t, out, "<img")
}

// The boundary a Markdown image is held to holds for one inside a block too.
func TestHTMLImgInsideMarkupBlockOutsideProjectIsRefused(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.png"), []byte("x"), 0o600))
	docs := filepath.Join(root, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	_, _, err = CompileMarkdown(
		[]byte("<p align=\"center\">\n  <img src=\"../secret.png\">\n</p>\n"),
		std, filepath.Join(docs, "doc.md"), types.MarkConfig{},
	)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
}
