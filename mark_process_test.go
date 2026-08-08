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

// writeFile writes content into dir and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestProcessFileFetchesAttachmentsOnce pins the fix for the duplicate
// GetAttachments. ProcessFile resolves attachments twice per page -- declared
// attachments, then diagrams found while rendering -- and each pass used to
// issue its own paginated fetch of the same remote list.
func TestProcessFileFetchesAttachmentsOnce(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	// A 1x1 PNG so the image-dimension probe has something valid to read.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o600))

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Attachment Doc -->
<!-- Attachment: logo.png -->

# Attachment Doc

![logo](logo.png)
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	server.ResetRequests()

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	assert.Equal(t, 1, server.CountRequests("GET", "/child/attachment"),
		"the remote attachment list should be fetched once per page")

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
}

// TestProcessFileCreatesAndUpdatesPage is a smoke test for the whole
// orchestration path, which had no coverage at all: metadata, ancestry, page
// creation, body upload and labels in one run.
func TestProcessFileCreatesAndUpdatesPage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: New Page -->
<!-- Label: alpha -->

# New Page

Some **content**.
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	stored := server.Page(target.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "New Page", stored.Title)
	assert.Contains(t, stored.Body, "<strong>content</strong>")
	assert.Equal(t, []string{"alpha"}, stored.Labels)

	// The page was created under the declared parent.
	parent, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	assert.Equal(t, parent.ID, stored.ParentID)
}
