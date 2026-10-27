package mark

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The recipe from the README, verbatim.
const widthMacro = `<!-- Macro: \!\[.*\]\((.+)\)\<\!\-\- width=(.*) \-\-\>
     Template: ac:image
     Attachment: ${1}
     Width: ${2} -->
`

// publishMacroDoc publishes a document with the width macro and returns the
// server, the page, and what was logged.
func publishMacroDoc(t *testing.T, header, body string) (*confluencetest.Server, string, string) {
	t.Helper()

	p := publishBeside(t, map[string][]byte{"images/example.png": onePixelPNG(), "logo.png": onePixelPNG()},
		header+"\n"+widthMacro, body, Config{})
	require.NoError(t, p.err)
	require.NotEmpty(t, p.id)

	return p.server, p.id, p.logged
}

// TestMacroAttachmentIsUploaded covers the README's width recipe: the macro
// names the file in its Attachment key, and the page must hold that file under
// the name the expansion wrote.
func TestMacroAttachmentIsUploaded(t *testing.T) {
	cases := map[string]struct {
		body     string
		filename string
	}{
		"a plain path":     {"![Logo](logo.png)<!-- width=300 -->\n", "logo.png"},
		"a nested path":    {"![Ex](images/example.png)<!-- width=300 -->\n", "images_example.png"},
		"a ./ path":        {"![Logo](./logo.png)<!-- width=300 -->\n", "logo.png"},
		"a ./ nested path": {"![Ex](./images/example.png)<!-- width=120 -->\n", "images_example.png"},
		"with a title":     {"![Logo](logo.png \"The logo\")<!-- width=300 -->\n", "logo.png"},
		"inside a table":   {"| a | b |\n|---|---|\n| ![Logo](logo.png)<!-- width=300 --> | text |\n", "logo.png"},
		"outside a table":  {"Text before.\n\n![Logo](logo.png)<!-- width=300 -->\n\nText after.\n", "logo.png"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server, id, _ := publishMacroDoc(t, "", tc.body)

			stored := server.Attachments(id)
			require.Len(t, stored, 1)
			assert.Equal(t, tc.filename, stored[0].Filename)

			body := server.Page(id).Body
			assert.Contains(t, body, `<ri:attachment ri:filename="`+tc.filename+`"/>`)
			assert.Contains(t, body, `ac:width=`)
		})
	}
}

// TestMacroAttachmentAlsoDeclaredIsNotUnused covers the document that already
// declares the file in a header: the macro is what uses it, so it must not be
// reported as unused, and it is still uploaded once.
func TestMacroAttachmentAlsoDeclaredIsNotUnused(t *testing.T) {
	server, id, logged := publishMacroDoc(t,
		"<!-- Attachment: logo.png -->\n", "![Logo](logo.png)<!-- width=300 -->\n")

	assert.NotContains(t, logged, "unused attachment")
	require.Len(t, server.Attachments(id), 1)
	assert.Equal(t, 1, server.CountRequests("POST", "/child/attachment"))
}

// TestMacroAttachmentUsedTwiceIsUploadedOnce covers a file that two matches
// name.
func TestMacroAttachmentUsedTwiceIsUploadedOnce(t *testing.T) {
	server, id, _ := publishMacroDoc(t, "",
		"![A](logo.png)<!-- width=10 -->\n\n![B](logo.png)<!-- width=20 -->\n")

	require.Len(t, server.Attachments(id), 1)
	assert.Equal(t, 1, server.CountRequests("POST", "/child/attachment"))
}

// TestMacroAttachmentOutsideProjectFails is the boundary every other upload
// has: a document does not get to publish whatever file it names.
func TestMacroAttachmentOutsideProjectFails(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	root := t.TempDir()
	dir := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.png"), onePixelPNG(), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n"+widthMacro+"\n# Doc\n\n![S](../../../secret.png)<!-- width=1 -->\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	t.Chdir(dir)

	_, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Output: io.Discard,
	})
	require.Error(t, err)
	assert.Zero(t, server.CountRequests("POST", "/child/attachment"))
}

// TestMacroAttachmentTitleReachingOutsideIsOnlyATitle: the title is not part
// of the file's name, so a "../" in it must not fail the run for an image that
// is sitting beside the document.
func TestMacroAttachmentTitleReachingOutsideIsOnlyATitle(t *testing.T) {
	server, id, _ := publishMacroDoc(t, "",
		"![L](logo.png \"see v1/../../../old\")<!-- width=1 -->\n")

	stored := server.Attachments(id)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
	assert.Contains(t, server.Page(id).Body, `<ri:attachment ri:filename="logo.png"/>`)
}

// TestMacroAttachmentDecodingToARootedPathIsWarnedAbout: "%2Fetc%2Fpasswd"
// decodes to a file that is never read, and the page still refers to an
// attachment, so the missing upload is said out loud rather than dropped.
func TestMacroAttachmentDecodingToARootedPathIsWarnedAbout(t *testing.T) {
	server, id, logged := publishMacroDoc(t, "", "![P](%2Fetc%2Fpasswd)<!-- width=1 -->\n")

	assert.Empty(t, server.Attachments(id))
	assert.Contains(t, logged, "is not uploaded")
	assert.Contains(t, logged, "%2Fetc%2Fpasswd")
}

// TestMacroAttachmentOnLegacyCompilePath keeps both compile paths in step.
func TestMacroAttachmentOnLegacyCompilePath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), onePixelPNG(), 0o600))
	path := filepath.Join(dir, "doc.md")

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	source := widthMacro + "\n![Logo](logo.png)<!-- width=300 -->\n"

	for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
		"default": markmd.CompileMarkdown,
		"legacy":  markmd.CompileMarkdownLegacy,
	} {
		t.Run(name, func(t *testing.T) {
			html, attached, err := compile([]byte(source), std, path, types.MarkConfig{})
			require.NoError(t, err)

			require.Len(t, attached, 1)
			assert.Equal(t, "logo.png", attached[0].Filename)
			assert.Contains(t, html, `<ri:attachment ri:filename="logo.png"/>`)
		})
	}
}

// TestMacroAttachmentThatIsAURLIsNotAFile covers a template that reuses the
// key for a remote image: there is nothing to upload and nothing to warn of.
func TestMacroAttachmentThatIsAURLIsNotAFile(t *testing.T) {
	server, id, logged := publishMacroDoc(t, "",
		"![A](https://example.com/a.png)<!-- width=10 -->\n")

	assert.NotContains(t, logged, "is not uploaded")
	assert.Empty(t, server.Attachments(id))
}

// TestMacroAttachmentThatIsNotBesideTheDocumentIsNotRead covers the values a
// project file of the same name must never be uploaded for.
func TestMacroAttachmentThatIsNotBesideTheDocumentIsNotRead(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	for name, value := range map[string]string{
		"mailto":            "mailto:user@example.com",
		"data":              "data:image/png,x",
		"https, no slashes": "https:host/path.png",
		"protocol-relative": "//host/path.png",
		"absolute":          "/etc/secret.png",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "doc.md")

			// Where a bare join of each value onto the directory would land.
			for _, file := range []string{"etc/secret.png", "host/path.png"} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), onePixelPNG(), 0o600))
			}

			source := widthMacro + "\n![A](" + value + ")<!-- width=10 -->\n"

			_, attached, err := markmd.CompileMarkdown([]byte(source), std, path, types.MarkConfig{})
			require.NoError(t, err)
			assert.Empty(t, attached)
		})
	}
}

// TestMacroAttachmentIsReadLikeAnImageDestination keeps a macro image in step
// with a plain one: "my%20logo.png" names "my logo.png", and the page has to
// say so under the name the file is uploaded as.
func TestMacroAttachmentIsReadLikeAnImageDestination(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	tests := map[string]struct {
		files []string
		value string
		want  string
	}{
		"decoded when only the decoded file exists": {
			files: []string{"my logo.png"}, value: "my%20logo.png", want: "my logo.png",
		},
		"as written when a file really has the percent": {
			files: []string{"my%20logo.png", "my logo.png"}, value: "my%20logo.png", want: "my%20logo.png",
		},
		"backslash escape": {
			files: []string{"my_logo.png"}, value: `my\_logo.png`, want: "my_logo.png",
		},
		"entity reference": {
			files: []string{"a&b.png"}, value: "a&amp;b.png", want: "a&b.png",
		},
		"escaped and percent-encoded": {
			files: []string{"my_logo 1.png"}, value: `my\_logo%201.png`, want: "my_logo 1.png",
		},
		"as written when nothing exists": {
			files: nil, value: "my%20logo.png", want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for _, file := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), onePixelPNG(), 0o600))
			}

			source := widthMacro + "\n![A](" + tt.value + ")<!-- width=10 -->\n"

			html, attached, err := markmd.CompileMarkdown(
				[]byte(source), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
			require.NoError(t, err)

			if tt.want == "" {
				assert.Empty(t, attached)

				return
			}

			require.Len(t, attached, 1)
			assert.Equal(t, tt.want, attached[0].Filename)
			assert.Contains(t, html, `ri:filename="`+strings.ReplaceAll(tt.want, "&", "&amp;")+`"`)
		})
	}
}

// TestMacroAttachmentThatIsAnAngleBracketedURLIsNotAFile: the brackets do not
// make a URL a file.
func TestMacroAttachmentThatIsAnAngleBracketedURLIsNotAFile(t *testing.T) {
	server, id, logged := publishMacroDoc(t, "",
		"![A](<https://example.com/a.png>)<!-- width=10 -->\n")

	assert.NotContains(t, logged, "is not uploaded")
	assert.Empty(t, server.Attachments(id))
}

// TestMacroAttachmentEscapedToARootedPathIsWarnedAbout: "\/etc/passwd" is
// "%2Fetc%2Fpasswd" spelled with a backslash escape, and is said out loud the
// same way.
func TestMacroAttachmentEscapedToARootedPathIsWarnedAbout(t *testing.T) {
	server, id, logged := publishMacroDoc(t, "", "![P](\\/etc/passwd)<!-- width=1 -->\n")

	assert.Empty(t, server.Attachments(id))
	assert.Contains(t, logged, "is not uploaded")
}

// TestMacroAttachmentWithAColonIsUploaded covers a filename that holds a colon,
// which Linux and macOS allow: it looks as if it opened with a URI scheme, but
// it is the file the macro writes into the page, so it has to be uploaded.
func TestMacroAttachmentWithAColonIsUploaded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows filename cannot hold a colon")
	}

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shot:1.png"), onePixelPNG(), 0o600))

	source := widthMacro + "\n![A](shot:1.png)<!-- width=10 -->\n"

	html, attached, err := markmd.CompileMarkdown(
		[]byte(source), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)
	require.Len(t, attached, 1)
	assert.Equal(t, "shot:1.png", attached[0].Filename)
	assert.Contains(t, html, `<ri:attachment ri:filename="shot:1.png"/>`)
}

// attachFixture publishes one document that links to a file beside it, and
// returns what the server ended up holding.
func attachFixture(t *testing.T, body string, attachReferenced bool) (*confluencetest.Server, string, string) {
	t.Helper()

	p := publishBeside(t, map[string][]byte{"files/report.pdf": []byte("a report\n")}, "", body,
		Config{AttachReferenced: attachReferenced})
	require.NoError(t, p.err)

	return p.server, p.id, p.body
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

// TestAFileLinkedTwiceIsAttachedOnce covers the page that does not have the file
// yet. Each link hands the file over again, and every copy used to be created,
// so Confluence refused the second one for a name the first had just taken, and
// the run failed on its first publish only.
func TestAFileLinkedTwiceIsAttachedOnce(t *testing.T) {
	for name, body := range map[string]string{
		"inline":    "See [one](files/report.pdf) and [two](files/report.pdf).\n",
		"reference": "See [one](files/report.pdf), [two][r] and [three][r].\n\n[r]: files/report.pdf\n",
	} {
		t.Run(name, func(t *testing.T) {
			server, id, page := attachFixture(t, body, true)

			stored := server.Attachments(id)
			require.Len(t, stored, 1)
			assert.Equal(t, "files_report.pdf", stored[0].Filename)
			assert.Equal(t, 1, server.CountRequests("POST", "/child/attachment"))
			assert.NotContains(t, page, `href="files/report.pdf"`)
		})
	}
}

// TestAnImageEmbeddedTwiceIsAttachedOnce is the same for an image, which takes
// the same way in without the flag.
func TestAnImageEmbeddedTwiceIsAttachedOnce(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), onePixelPNG(), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\n![logo](logo.png)\n\n![again](logo.png)\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Output: io.Discard,
	})
	require.NoError(t, err)

	require.Len(t, server.Attachments(target.ID), 1)
	assert.Equal(t, 1, server.CountRequests("POST", "/child/attachment"))
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

// TestAFilenameWithBracketsIsNotAPattern covers the destination that reads as a
// glob and is not one. A file really called report[2024].pdf is an ordinary
// name; expanded as a pattern it names one of report2.pdf, report0.pdf and
// report4.pdf instead, so the page would carry a link that reads as the report
// and downloads whatever else happened to be sitting beside it. A link points
// at one file, and it is the one it names.
func TestAFilenameWithBracketsIsNotAPattern(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "files", "report[2024].pdf"), []byte("the one linked to\n"), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "files", "report2.pdf"), []byte("what the glob would match\n"), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\n"+
			"See [the report](<files/report[2024].pdf>).\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Output: io.Discard,
	})
	require.NoError(t, err)

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "files_report[2024].pdf", stored[0].Filename)
	assert.Contains(t, server.Page(target.ID).Body, `ri:filename="files_report[2024].pdf"`)
}

// TestALinkedFileWithAColonIsAttached covers a filename that holds a colon,
// which Linux and macOS allow. What comes before the colon is shaped like a URI
// scheme, but it is no scheme a link is written with, so the link names a file
// beside the document.
func TestALinkedFileWithAColonIsAttached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows filename cannot hold a colon")
	}

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes:v2.pdf"), []byte("notes\n"), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\nSee [the spec](notes:v2.pdf).\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Output: io.Discard,
	})
	require.NoError(t, err)

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "notes:v2.pdf", stored[0].Filename)
	assert.Contains(t, server.Page(target.ID).Body, `<ri:attachment ri:filename="notes:v2.pdf"`)
}

// published is what publishBeside leaves behind.
type published struct {
	server *confluencetest.Server
	id     string
	body   string
	logged string
	err    error
}

// publishBeside publishes a document beside files, given by path and content,
// with header lines after its Space and Title.
func publishBeside(t *testing.T, files map[string][]byte, header, body string, cfg Config) published {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, content, 0o600))
	}

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n"+header+"\n# Doc\n\n"+body)

	cfg.BaseURL, cfg.Username, cfg.Password = server.URL, "user", "token"
	cfg.Files, cfg.Output = file, io.Discard

	api := confluence.NewAPI(server.URL, "user", "token", false)
	var target *confluence.PageInfo
	var err error
	logged := captureLog(t, func() { target, err = ProcessFile(file, api, cfg) })

	result := published{server: server, logged: logged, err: err}
	if target != nil {
		result.id = target.ID
		result.body = server.Page(target.ID).Body
	}

	return result
}

// pageFilenames is every attachment name the page body refers to.
func pageFilenames(body string) []string {
	var names []string
	for _, match := range riFilename.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(names, match[1]) {
			names = append(names, match[1])
		}
	}

	return names
}

func uploadedNames(p published) []string {
	var names []string
	for _, a := range p.server.Attachments(p.id) {
		names = append(names, a.Filename)
	}

	return names
}

func spellingFiles() map[string][]byte {
	return map[string][]byte{"images/x.png": onePixelPNG(), "f.txt": []byte("text\n")}
}

// TestEverySpellingOfAFileIsOneAttachment covers one file written several
// ways: it is uploaded once, and the page names it as uploaded.
func TestEverySpellingOfAFileIsOneAttachment(t *testing.T) {
	cases := map[string]struct{ body, name string }{
		"image ./":       {"![a](images/x.png)\n\n![b](./images/x.png)\n", "images_x.png"},
		"link ./":        {"[a](f.txt) and [b](./f.txt)\n", "f.txt"},
		"image and link": {"![a](images/x.png)\n\n[b](./images/x.png)\n", "images_x.png"},
		"double slash":   {"![a](images/x.png)\n\n![b](images//x.png)\n", "images_x.png"},
		"dot dot":        {"![a](images/x.png)\n\n![b](images/../images/x.png)\n", "images_x.png"},
		"lone ./ image":  {"![a](./images/x.png)\n", "images_x.png"},
		"lone ./ link":   {"[a](./f.txt)\n", "f.txt"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := publishBeside(t, spellingFiles(), "", tc.body, Config{AttachReferenced: true})
			require.NoError(t, p.err)

			assert.Equal(t, []string{tc.name}, uploadedNames(p))
			assert.Equal(t, []string{tc.name}, pageFilenames(p.body))
			assert.Equal(t, 1, p.server.CountRequests("POST", "/child/attachment"))
		})
	}
}

// TestADeclaredFileLinkedUnderAnotherSpelling covers a header that declares a
// file one way and a link that names it another: the link leads to the
// attachment, and the attachment is not reported unused.
func TestADeclaredFileLinkedUnderAnotherSpelling(t *testing.T) {
	cases := map[string]struct{ declared, linked string }{
		"declared ./": {"./f.txt", "f.txt"},
		"linked ./":   {"f.txt", "./f.txt"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := publishBeside(t, spellingFiles(),
				"<!-- Attachment: "+tc.declared+" -->\n", "[a]("+tc.linked+")\n", Config{})
			require.NoError(t, p.err)

			assert.Equal(t, []string{"f.txt"}, uploadedNames(p))
			assert.NotContains(t, p.body, `href="`+tc.linked+`"`)
			assert.Contains(t, p.body, "/download/attachments/")
			assert.NotContains(t, p.logged, "unused attachment")
		})
	}
}

// TestDifferentFilesUnderOneNameAreRefused covers a declared file and one the
// document refers to that upload under one name: the second would replace the
// first on the page, so the run fails before it is uploaded.
func TestDifferentFilesUnderOneNameAreRefused(t *testing.T) {
	files := map[string][]byte{
		"a/b.txt": []byte("declared\n"), "a_b.txt": []byte("linked\n"),
		"a/b.png": onePixelPNG(), "a_b.png": append(onePixelPNG(), 0),
	}

	cases := map[string]struct {
		declared, body string
		cfg            Config
	}{
		"a linked file":        {"a/./b.txt", "[x](a_b.txt)\n", Config{AttachReferenced: true}},
		"an image":             {"a/b.png", "![x](a_b.png)\n", Config{}},
		"an image, dry run":    {"a/b.png", "![x](a_b.png)\n", Config{DryRun: true}},
		"a link, compile only": {"a//b.txt", "[x](a_b.txt)\n", Config{AttachReferenced: true, CompileOnly: true}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := publishBeside(t, files, "<!-- Attachment: "+tc.declared+" -->\n", tc.body, tc.cfg)
			require.Error(t, p.err)
			assert.Contains(t, p.err.Error(), "would both be uploaded as")
			assert.Zero(t, p.server.CountRequests("POST", "/data"), "the declared file was replaced")
		})
	}
}

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

var riFilename = regexp.MustCompile(`ri:filename="([^"]*)"`)

// TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses: the ac:image
// template flattens a "/" in an attachment's filename, and a diagram's
// filename is its title. A diagram titled Auth/Login was uploaded as
// Auth/Login.svg and shown as Auth_Login.svg, which is no attachment at all:
// the page showed a broken image.
func TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses(t *testing.T) {
	// A title is text, not a path: only its slashes change, and "./" in it
	// is kept.
	for title, want := range map[string]string{
		"Auth/Login": "Auth_Login.svg", "./x": "._x.svg", "Q1/./Q2": "Q1_._Q2.svg",
	} {
		t.Run(title, func(t *testing.T) {
			p := publishBeside(t, nil, "", "```d2 title "+title+"\na -> b\n```\n", Config{
				Features: []string{"d2"}, D2Output: "svg", D2Scale: 1,
			})
			require.NoError(t, p.err)

			stored := uploadedNames(p)
			require.Len(t, stored, 1)

			m := riFilename.FindStringSubmatch(p.body)
			require.NotNil(t, m, p.body)
			assert.Equal(t, want, m[1])
			assert.Equal(t, m[1], stored[0], "the page names the attachment that was uploaded")
		})
	}
}

// mermaidFixture returns a server and a configuration that publishes one
// ordinary document, so that what a test changes about the mermaid settings is
// the only thing that can fail the run.
func mermaidFixture(t *testing.T) Config {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	return Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    filepath.Join(dir, "*.md"),
		Output:   io.Discard,
	}
}

// TestMermaidOutputRejectsAFormatMarkCannotPublish covers the settings from the
// side the flags do not reach. Config is a public API, so a library caller
// arrives here without a command line to have been checked, and a format the
// renderer does not know falls to its default branch: a PNG, published without
// a word about the SVG that was asked for.
func TestMermaidOutputRejectsAFormatMarkCannotPublish(t *testing.T) {
	config := mermaidFixture(t)
	config.MermaidOutput = "jpeg"

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "MermaidOutput")
}

// TestMermaidBundleNeedsAnSVGToGoIn is the same for the pair of them: a bundle
// is kept inside the SVG, so asking for one alongside a PNG asks for something
// that cannot happen, and silently getting the PNG is the worst reading of it.
func TestMermaidBundleNeedsAnSVGToGoIn(t *testing.T) {
	config := mermaidFixture(t)
	config.MermaidBundle = true

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "MermaidBundle")
}

// TestMermaidDefaultsPublishAsBefore is the control, and the reason the empty
// value is accepted rather than checked against the two names: a caller that
// never heard of these fields leaves them zero, and has always got a PNG.
func TestMermaidDefaultsPublishAsBefore(t *testing.T) {
	require.NoError(t, Run(mermaidFixture(t)))
}

// TestD2OutputRejectsAFormatMarkCannotPublish is the d2 half of the same rule:
// Config is a public API, so a library caller arrives with no command line to
// have been checked, and a format the renderer does not know falls to its PNG
// branch without a word.
func TestD2OutputRejectsAFormatMarkCannotPublish(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Output = "jpeg"

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "D2Output")
}

// TestD2DefaultsPublishAsBefore is its control: a caller that never heard of
// the field leaves it zero, and has always got a PNG.
func TestD2DefaultsPublishAsBefore(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Output = ""

	require.NoError(t, Run(config))
}

// TestD2ScaleRejectsWhatIsNotAScale covers the public API for the same numbers.
// NaN fails every comparison it is given, so a check phrased as "not <= 0" lets
// it through; an infinity passes one outright. Both then multiply a diagram's
// size into something that is not a number of pixels.
func TestD2ScaleRejectsWhatIsNotAScale(t *testing.T) {
	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		config := mermaidFixture(t)
		config.Features = []string{"d2"}
		config.D2Scale = scale

		err := Run(config)
		require.Error(t, err, "a scale of %v is not one", scale)
		assert.Contains(t, err.Error(), "D2Scale")
	}
}

// TestMermaidAndMathScaleRejectWhatIsNotAScale covers the public API for the
// other two scales. Mermaid sent any number to the browser, where a negative
// one waited out the render timeout and NaN or an infinity could not be sent at
// all; math let NaN past the "<= 0" meant to catch it.
func TestMermaidAndMathScaleRejectWhatIsNotAScale(t *testing.T) {
	for _, scale := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		config := mermaidFixture(t)
		config.Features = []string{"mermaid"}
		config.MermaidScale = scale

		err := Run(config)
		require.Error(t, err, "a mermaid scale of %v is not one", scale)
		assert.Contains(t, err.Error(), "MermaidScale")

		config = mermaidFixture(t)
		config.Features = []string{"math"}
		config.MathScale = scale

		err = Run(config)
		require.Error(t, err, "a math scale of %v is not one", scale)
		assert.Contains(t, err.Error(), "MathScale")
	}
}

// TestMermaidAndMathScaleZeroIsTheDefault is where they part from d2: mermaid
// is on by default, so refusing zero would refuse every Config that never heard
// of the field, and math has always drawn a zero scale at its default.
func TestMermaidAndMathScaleZeroIsTheDefault(t *testing.T) {
	config := mermaidFixture(t)
	config.Features = []string{"mermaid", "math"}

	require.NoError(t, Run(config))
}

// TestMermaidAndMathScaleAreOnlyCheckedWhereTheyAreDrawn: a run with neither
// feature carries both fields past every path that reads them.
func TestMermaidAndMathScaleAreOnlyCheckedWhereTheyAreDrawn(t *testing.T) {
	config := mermaidFixture(t)
	config.Features = []string{"mention"}
	config.MermaidScale = -1
	config.MathScale = math.NaN()

	require.NoError(t, Run(config))
}

// TestD2ScaleIsOnlyCheckedWhereDiagramsAreDrawn is the boundary: a run that
// never turns d2 on carries the field past every path that reads it, so the
// zero value of a caller who never heard of it is not an error.
func TestD2ScaleIsOnlyCheckedWhereDiagramsAreDrawn(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Scale = 0

	require.NoError(t, Run(config))
}
