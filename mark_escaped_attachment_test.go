package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A destination is a URL, so a file whose name holds a character URLs give a
// meaning to has to be written escaped: a#b.png as a%23b.png, 100%.pdf as
// 100%25.pdf. These pin that such a destination reaches the file it names, and
// that the attachment uploaded for it and the link to it keep the name intact.

// TestDeclaredAttachmentWithEscapedName is the download link: Confluence
// percent-encodes the filename in it, and decoding that turned a link to
// a#b.png into a link to "a" with a fragment.
func TestDeclaredAttachmentWithEscapedName(t *testing.T) {
	p := publishBeside(t,
		map[string][]byte{"a#b.txt": []byte("hash\n"), "my file.txt": []byte("space\n")},
		"<!-- Attachment: a#b.txt -->\n<!-- Attachment: my file.txt -->\n",
		"[hash](a%23b.txt) [hash as declared](<a#b.txt>) [space](my%20file.txt) [space as declared](<my file.txt>)\n",
		Config{},
	)
	require.NoError(t, p.err)

	assert.ElementsMatch(t, []string{"a#b.txt", "my file.txt"}, uploadedNames(p))

	assert.Contains(t, p.body, `/download/attachments/`+p.id+`/a%23b.txt"`)
	assert.Contains(t, p.body, `/download/attachments/`+p.id+`/my%20file.txt"`)
	assert.NotContains(t, p.body, `a#b.txt"`, "the # has to stay escaped in the link")
}

// TestInlineImageWithEscapedName covers an image that is not declared: it is
// found on disk by the name the destination decodes to.
func TestInlineImageWithEscapedName(t *testing.T) {
	p := publishBeside(t,
		map[string][]byte{"a#b.png": []byte("not really a png\n"), "100%.png": []byte("percent\n")},
		"", "![hash](a%23b.png) ![percent](100%25.png)\n", Config{},
	)
	require.NoError(t, p.err)

	assert.ElementsMatch(t, []string{"a#b.png", "100%.png"}, uploadedNames(p))

	assert.Contains(t, p.body, `ri:filename="a#b.png"`)
	assert.Contains(t, p.body, `ri:filename="100%.png"`)
}

// TestLiteralPercentNameWinsOverDecoded keeps a file really called
// my%20file.png reachable: the destination as written is tried first.
func TestLiteralPercentNameWinsOverDecoded(t *testing.T) {
	p := publishBeside(t,
		map[string][]byte{"my%20file.png": []byte("literal\n"), "my file.png": []byte("decoded\n")},
		"", "![literal](my%20file.png)\n", Config{},
	)
	require.NoError(t, p.err)

	assert.Equal(t, []string{"my%20file.png"}, uploadedNames(p))
}

// TestReferencedFileWithEscapedName is the --attach-referenced path.
func TestReferencedFileWithEscapedName(t *testing.T) {
	p := publishBeside(t,
		map[string][]byte{"q#1.pdf": []byte("report\n")},
		"", "See [the report](q%231.pdf).\n", Config{AttachReferenced: true},
	)
	require.NoError(t, p.err)

	assert.Equal(t, []string{"q#1.pdf"}, uploadedNames(p))
	assert.Contains(t, p.body, `ri:filename="q#1.pdf"`)
}

// TestInlineImageWithBracketsInItsName: an image was resolved as a pattern, so
// ![x](<img[1].png>) matched img1.png beside it and published that in its
// place -- a different picture under a name that reads as right. An image
// names one file, as a link does.
func TestInlineImageWithBracketsInItsName(t *testing.T) {
	p := publishBeside(t,
		map[string][]byte{"img[1].png": []byte("the one named\n"), "img1.png": []byte("what the glob matched\n")},
		"", "![x](<img[1].png>)\n", Config{},
	)
	require.NoError(t, p.err)

	assert.Equal(t, []string{"img[1].png"}, uploadedNames(p))
	assert.Contains(t, p.body, `ri:filename="img[1].png"`)
}
