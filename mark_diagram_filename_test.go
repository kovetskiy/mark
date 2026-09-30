package mark

import (
	"io"
	"regexp"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var riFilename = regexp.MustCompile(`ri:filename="([^"]*)"`)

// TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses: the ac:image
// template flattens a "/" in an attachment's filename, and a diagram's
// filename is its title. A diagram titled Auth/Login was uploaded as
// Auth/Login.svg and shown as Auth_Login.svg, which is no attachment at all:
// the page showed a broken image.
func TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n```d2 title Auth/Login\na -> b\n```\n")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Output: io.Discard,
		Features: []string{"d2"}, D2Output: "svg", D2Scale: 1,
	})
	require.NoError(t, err)

	body := server.Page(target.ID).Body
	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)

	m := riFilename.FindStringSubmatch(body)
	require.NotNil(t, m, body)
	assert.Equal(t, "Auth_Login.svg", m[1])
	assert.Equal(t, m[1], stored[0].Filename, "the page names the attachment that was uploaded")
}
