package mark

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "images", "example.png"), onePixelPNG(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), onePixelPNG(), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n"+header+"\n"+widthMacro+"\n# Doc\n\n"+body)

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Output: io.Discard,
	})
	require.NoError(t, err)
	require.NotNil(t, target)

	return server, target.ID, logged.String()
}

// TestMacroAttachmentIsUploaded covers the README's width recipe: the macro
// names the file in its Attachment key, and the page must hold that file under
// the name the expansion wrote.
func TestMacroAttachmentIsUploaded(t *testing.T) {
	cases := map[string]struct {
		body     string
		filename string
	}{
		"a plain path":    {"![Logo](logo.png)<!-- width=300 -->\n", "logo.png"},
		"a nested path":   {"![Ex](images/example.png)<!-- width=300 -->\n", "images_example.png"},
		"a ./ path":       {"![Logo](./logo.png)<!-- width=300 -->\n", "._logo.png"},
		"a parent path":   {"![Ex](./images/example.png)<!-- width=120 -->\n", "._images_example.png"},
		"inside a table":  {"| a | b |\n|---|---|\n| ![Logo](logo.png)<!-- width=300 --> | text |\n", "logo.png"},
		"outside a table": {"Text before.\n\n![Logo](logo.png)<!-- width=300 -->\n\nText after.\n", "logo.png"},
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

// TestMacroAttachmentOnLegacyCompilePath keeps both compile paths in step.
func TestMacroAttachmentOnLegacyCompilePath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), onePixelPNG(), 0o600))
	path := filepath.Join(dir, "doc.md")

	std, err := stdlib.New(nil)
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
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	for name, value := range map[string]string{
		"mailto":            "mailto:user@example.com",
		"data":              "data:image/png,x",
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
