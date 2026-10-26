package mark

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	cfg.BaseURL, cfg.Username, cfg.Password = server.URL, "user", "token"
	cfg.Files, cfg.Output = file, io.Discard

	api := confluence.NewAPI(server.URL, "user", "token", false)
	target, err := ProcessFile(file, api, cfg)

	result := published{server: server, logged: logged.String(), err: err}
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
