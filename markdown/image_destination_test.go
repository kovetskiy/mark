package mark

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compilers is both compile paths, since both render images through the same
// renderer and invariant 6 says both have to keep working.
var compilers = map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
	"default": CompileMarkdown,
	"legacy":  CompileMarkdownLegacy,
}

// imageDocument lays out a directory holding the given image files, each a
// copy of testdata/test.png, and returns the path of a document inside it.
func imageDocument(t *testing.T, names ...string) string {
	t.Helper()

	_, self, _, _ := runtime.Caller(0)
	png, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "testdata", "test.png"))
	require.NoError(t, err)

	dir := t.TempDir()
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), png, 0o600))
	}

	return filepath.Join(dir, "doc.md")
}

// An image destination is a URL, so "my%20file.png" and "my\_file.png" name the
// same files "<my file.png>" and "my_file.png" do. They were looked up as
// written, found nothing, and were published as a relative ri:url -- a broken
// image, with the file never uploaded.
func TestLocalImageDestinationIsDecoded(t *testing.T) {
	for name, compile := range compilers {
		for _, tc := range []struct {
			markdown string
			filename string
			htmlTag  bool
		}{
			{"![a](my%20file.png)\n", "my file.png", false},
			{"![a](<my file.png>)\n", "my file.png", false},
			{"![c](my\\_file.png)\n", "my_file.png", false},
			{`<img src="my%20file.png" alt="a">` + "\n", "my file.png", true},
		} {
			t.Run(name+" "+tc.markdown, func(t *testing.T) {
				// The legacy path has never converted <img> at all.
				if tc.htmlTag && name == "legacy" {
					t.Skip("the legacy path leaves <img> as it is")
				}

				doc := imageDocument(t, "my file.png", "my_file.png")

				std, err := stdlib.New(nil)
				require.NoError(t, err)

				out, attachments, err := compile([]byte(tc.markdown), std, doc, types.MarkConfig{})
				require.NoError(t, err)

				assert.Contains(t, out, `<ri:attachment ri:filename="`+tc.filename+`"/>`)
				assert.NotContains(t, out, "ri:url")

				require.Len(t, attachments, 1, "the file has to be uploaded as well as referenced")
				assert.Equal(t, tc.filename, attachments[0].Filename)
			})
		}
	}
}

// A file whose name really contains a percent sign is still found by the name
// it was written with.
func TestLocalImageWithALiteralPercentIsStillFound(t *testing.T) {
	doc := imageDocument(t, "100%25.png")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte("![a](100%25.png)\n"), std, doc, types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, out, `<ri:attachment ri:filename="100%25.png"/>`)
}

// A remote URL is left as it is: percent-encoding is part of what it says.
func TestRemoteImageDestinationKeepsItsEncoding(t *testing.T) {
	for name, compile := range compilers {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile([]byte("![a](https://example.com/my%20file.png)\n"), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, `<ri:url ri:value="https://example.com/my%20file.png"/>`)
		})
	}
}

// A title and alt are read as Markdown before being escaped once for XML. They
// were escaped as written, so the backslashes stayed and an entity reference
// was escaped a second time.
func TestImageTitleAndAltAreUnescapedOnce(t *testing.T) {
	for name, compile := range compilers {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile(
				[]byte(`![a \"b\" &amp; c](https://example.com/x.png "T \"q\" &amp; z")`+"\n"),
				std, "test.md", types.MarkConfig{},
			)
			require.NoError(t, err)

			assert.Contains(t, out, `ac:title="T &#34;q&#34; &amp; z"`)
			assert.Contains(t, out, `ac:alt="a &#34;b&#34; &amp; c"`)
			assert.NotContains(t, out, `\`)
			assert.NotContains(t, out, "&amp;amp;")
		})
	}
}

// An <img> is decoded by the HTML parser that finds it, so its title is not
// read as Markdown again: "&amp;amp;" in the tag is "&amp;" on the page.
func TestHTMLImgTitleIsNotDecodedTwice(t *testing.T) {
	out := compileImgDoc(t, `<img src="https://example.com/x.png" title="a &amp;amp; b \_c">`+"\n")

	assert.Contains(t, out, `ac:title="a &amp;amp; b \_c"`)
}

// A declared attachment is matched by the same decoded name, so the image
// points at the upload instead of being attached a second time beside it and
// the declared one being reported as unused.
func TestDeclaredAttachmentMatchesADecodedImageDestination(t *testing.T) {
	var asked []string
	resolve := func(target string) string {
		asked = append(asked, target)
		if target == "my file.png" {
			return "/download/attachments/1/my file.png?api=v2"
		}

		return ""
	}

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown([]byte("![a](my%20file.png)\n"), std, "test.md",
		types.MarkConfig{ResolveAttachment: resolve})
	require.NoError(t, err)

	assert.Contains(t, asked, "my file.png")
	assert.Contains(t, out, `<ri:url ri:value="/download/attachments/1/my file.png?api=v2"/>`)
}
