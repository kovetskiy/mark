package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishEscaped publishes one document beside the given files and returns the
// server and the page it wrote.
func publishEscaped(t *testing.T, files map[string]string, body string, attachReferenced bool) (*confluencetest.Server, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n"+body)

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: attachReferenced, Output: io.Discard,
	})
	require.NoError(t, err)

	return server, target.ID
}

// A destination is a URL, so a file whose name holds a character URLs give a
// meaning to has to be written escaped: a#b.png as a%23b.png, 100%.pdf as
// 100%25.pdf. These pin that such a destination reaches the file it names, and
// that the attachment uploaded for it and the link to it keep the name intact.

// TestDeclaredAttachmentWithEscapedName is the download link: Confluence
// percent-encodes the filename in it, and decoding that turned a link to
// a#b.png into a link to "a" with a fragment.
func TestDeclaredAttachmentWithEscapedName(t *testing.T) {
	server, pageID := publishEscaped(t,
		map[string]string{"a#b.txt": "hash\n", "my file.txt": "space\n"},
		"<!-- Attachment: a#b.txt -->\n<!-- Attachment: my file.txt -->\n\n"+
			"[hash](a%23b.txt) [hash as declared](<a#b.txt>) [space](my%20file.txt) [space as declared](<my file.txt>)\n",
		false,
	)

	var names []string
	for _, a := range server.Attachments(pageID) {
		names = append(names, a.Filename)
	}
	assert.ElementsMatch(t, []string{"a#b.txt", "my file.txt"}, names)

	body := server.Page(pageID).Body
	assert.Contains(t, body, `/download/attachments/`+pageID+`/a%23b.txt"`)
	assert.Contains(t, body, `/download/attachments/`+pageID+`/my%20file.txt"`)
	assert.NotContains(t, body, `a#b.txt"`, "the # has to stay escaped in the link")
}

// TestInlineImageWithEscapedName covers an image that is not declared: it is
// found on disk by the name the destination decodes to.
func TestInlineImageWithEscapedName(t *testing.T) {
	server, pageID := publishEscaped(t,
		map[string]string{"a#b.png": "not really a png\n", "100%.png": "percent\n"},
		"\n![hash](a%23b.png) ![percent](100%25.png)\n",
		false,
	)

	var names []string
	for _, a := range server.Attachments(pageID) {
		names = append(names, a.Filename)
	}
	assert.ElementsMatch(t, []string{"a#b.png", "100%.png"}, names)

	body := server.Page(pageID).Body
	assert.Contains(t, body, `ri:filename="a#b.png"`)
	assert.Contains(t, body, `ri:filename="100%.png"`)
}

// TestLiteralPercentNameWinsOverDecoded keeps a file really called
// my%20file.png reachable: the destination as written is tried first.
func TestLiteralPercentNameWinsOverDecoded(t *testing.T) {
	server, pageID := publishEscaped(t,
		map[string]string{"my%20file.png": "literal\n", "my file.png": "decoded\n"},
		"\n![literal](my%20file.png)\n",
		false,
	)

	stored := server.Attachments(pageID)
	require.Len(t, stored, 1)
	assert.Equal(t, "my%20file.png", stored[0].Filename)
}

// TestReferencedFileWithEscapedName is the --attach-referenced path.
func TestReferencedFileWithEscapedName(t *testing.T) {
	server, pageID := publishEscaped(t,
		map[string]string{"q#1.pdf": "report\n"},
		"\nSee [the report](q%231.pdf).\n",
		true,
	)

	stored := server.Attachments(pageID)
	require.Len(t, stored, 1)
	assert.Equal(t, "q#1.pdf", stored[0].Filename)
	assert.Contains(t, server.Page(pageID).Body, `ri:filename="q#1.pdf"`)
}
