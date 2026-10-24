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

// dotSlashFixture publishes body beside images/x.png and f.txt and returns the
// attachment names the server ended up with and the page body.
func dotSlashFixture(t *testing.T, body string) ([]string, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "images", "x.png"), onePixelPNG(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("text\n"), 0o600))

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n# Doc\n\n"+body)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Output: io.Discard,
	})
	require.NoError(t, err)

	var names []string
	for _, a := range server.Attachments(target.ID) {
		names = append(names, a.Filename)
	}
	return names, server.Page(target.ID).Body
}

// TestADotSlashSpellingIsTheSameFile covers a file named both "x" and "./x".
func TestADotSlashSpellingIsTheSameFile(t *testing.T) {
	for name, body := range map[string]string{
		"image twice":    "![a](images/x.png)\n\n![b](./images/x.png)\n",
		"link twice":     "[a](f.txt) and [b](./f.txt)\n",
		"image and link": "![a](images/x.png)\n\n[b](./images/x.png)\n",
	} {
		t.Run(name, func(t *testing.T) {
			names, page := dotSlashFixture(t, body)
			require.Len(t, names, 1)
			assert.NotContains(t, names[0], "._")
			assert.NotContains(t, page, `ri:filename="._`)
		})
	}
}

// TestALoneDotSlashReferenceHasNoDotSlashName: "./x" is named as "x" would be.
func TestALoneDotSlashReferenceHasNoDotSlashName(t *testing.T) {
	names, page := dotSlashFixture(t, "![a](./images/x.png)\n\n[b](./f.txt)\n")
	assert.ElementsMatch(t, []string{"images_x.png", "f.txt"}, names)
	assert.Contains(t, page, `ri:filename="images_x.png"`)
	assert.Contains(t, page, `ri:filename="f.txt"`)
}
