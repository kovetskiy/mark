package mark

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attachFixture publishes one document that links to a file beside it, and
// returns what the server ended up holding.
func attachFixture(t *testing.T, body string, attachReferenced bool) (*confluencetest.Server, string, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "files", "report.pdf"), []byte("a report\n"), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\n"+body)

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:            file,
		AttachReferenced: attachReferenced,
		Output:           io.Discard,
	})
	require.NoError(t, err)
	require.NotNil(t, target)

	return server, target.ID, server.Page(target.ID).Body
}

// TestLinkedFileIsAttached covers what the flag is for. A link to a file beside
// the document is published as the path the document wrote, which means nothing
// once the page is on Confluence: the reader gets a link that leads nowhere.
func TestLinkedFileIsAttached(t *testing.T) {
	server, id, body := attachFixture(t, "See [the report](files/report.pdf).\n", true)

	stored := server.Attachments(id)
	require.Len(t, stored, 1)
	assert.Equal(t, "files_report.pdf", stored[0].Filename)

	assert.Contains(t, body, `<ri:attachment ri:filename="files_report.pdf"`)
	assert.Contains(t, body, "the report", "the words between the brackets are the link text")
	assert.NotContains(t, body, `href="files/report.pdf"`)
}

// TestLinkedFileIsLeftAloneByDefault is the boundary: this changes what a
// published page contains, so nothing happens without being asked.
func TestLinkedFileIsLeftAloneByDefault(t *testing.T) {
	server, id, body := attachFixture(t, "See [the report](files/report.pdf).\n", false)

	assert.Empty(t, server.Attachments(id))
	assert.Contains(t, body, `href="files/report.pdf"`)
}

// TestOnlyFilesThatAreThereAreAttached covers the destinations that are not a
// file beside the document. Each is left exactly as it would have been.
func TestOnlyFilesThatAreThereAreAttached(t *testing.T) {
	for name, body := range map[string]string{
		"a URL":          "See [a site](https://example.com/a.pdf).\n",
		"an anchor":      "See [above](#doc).\n",
		"a mail address": "Write to [us](mailto:someone@example.com).\n",
		"a rooted path":  "See [it](/files/report.pdf).\n",
		"nothing there":  "See [missing](files/absent.pdf).\n",
	} {
		t.Run(name, func(t *testing.T) {
			server, id, _ := attachFixture(t, body, true)

			assert.Empty(t, server.Attachments(id))
		})
	}
}

// TestALinkToAnotherDocumentIsNotAnAttachment covers the destination that looks
// most like a file and is not one: another document is how a page refers to a
// page, and publishing a colleague's source as a download is not what linking
// to it meant.
func TestALinkToAnotherDocumentIsNotAnAttachment(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "other.md", "Some notes with no metadata.\n")
	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\nSee [other](other.md).\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Output: io.Discard,
	})
	require.NoError(t, err)

	assert.Empty(t, server.Attachments(target.ID))
	assert.Contains(t, server.Page(target.ID).Body, "other.md")
}

// TestAnImageIsAttachedWithoutTheFlag pins what was already true, so that the
// flag is understood to be about links: an image left as a path is visibly
// broken, where a link merely leads nowhere, and images have always been
// uploaded.
func TestAnImageIsAttachedWithoutTheFlag(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), onePixelPNG(), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\n![logo](logo.png)\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Output: io.Discard,
	})
	require.NoError(t, err)

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
}

// TestLinkedFileOutsideTheProjectIsRefused: a link is written by a document,
// and a document is content. The boundary is the one attachments have always
// been held to.
func TestLinkedFileOutsideTheProjectIsRefused(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.pem"), []byte("key"), 0o600))

	project := t.TempDir()
	docs := filepath.Join(project, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))
	t.Chdir(project)

	reference := filepath.Join("..", "..", filepath.Base(outside), "secret.pem")
	file := writeFile(t, docs, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\nSee ["+"key"+"]("+reference+").\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "outside the project")
}

// onePixelPNG is the smallest valid PNG, for a test that needs the dimension
// probe to have something real to read.
func onePixelPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
}
