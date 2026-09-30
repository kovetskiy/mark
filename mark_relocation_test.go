package mark

import (
	"io"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishOnce runs one file through ProcessFile against the fake, with a
// fresh client so nothing is answered from an earlier lookup's cache.
func publishOnce(t *testing.T, server *confluencetest.Server, file string) {
	t.Helper()

	api := confluence.NewAPI(server.URL, "user", "token", false)
	_, err := ProcessFile(file, api, Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Output:   io.Discard,
	})
	require.NoError(t, err)
}

// TestAPageOutsideItsParentsIsMovedIntoItsFolder: a page that already exists,
// found by title somewhere other than under its declared Parent, is the page
// the document means -- a title is unique within a space. Treating it as a
// stranger had mark create a second page with the same title, which
// Confluence refuses, instead of moving the one that is there.
func TestAPageOutsideItsParentsIsMovedIntoItsFolder(t *testing.T) {
	server, _ := docsSpace(t)
	home := server.Page("1002")
	require.NotNil(t, home)
	require.Equal(t, "Home", home.Title)
	doc := server.AddPage("DOCS", "Doc", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownInFolder("Manuals", "Doc"))

	publishOnce(t, server, file)

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Equal(t, 1, countPagesTitled(t, server, "Doc"))
	assert.Equal(t, folders[0].ID, server.Page(doc.ID).ParentID,
		"the existing page should have been moved into its folder")
	assert.Contains(t, server.Page(doc.ID).Body, "Body.", "and published in place")
}
