package mark

import (
	"io"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPreserveCommentsKeepsARetitle: the new title is staged on the page object
// and published by the update. --preserve-comments refetches the page in
// between, which brought back the title Confluence still held, and the rename
// was dropped without a word.
//
// It compounded: the manifest went on to record the new title against a page
// carrying the old one, so every parent named by that title resolved to the
// wrong place from then on, and under --changes-only the page was stuck under
// its old name for good.
func TestPreserveCommentsKeepsARetitle(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Original Title")))
	config.PreserveComments = true
	require.NoError(t, Run(config))

	original, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, original)

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Renamed Title", server.Page(original.ID).Title,
		"the page should have been renamed in place")
	assert.Equal(t, 0, countPagesTitled(t, server, "Original Title"))
}

// TestPreserveCommentsLeavesAnUnchangedTitleAlone is the control: the fix has
// to restore the title that was staged, not stamp the document's title onto
// every page it touches.
func TestPreserveCommentsLeavesAnUnchangedTitleAlone(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Steady")))
	config.PreserveComments = true

	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	assert.Equal(t, 1, countPagesTitled(t, server, "Steady"))
}

// TestGeneratedHashDoesNotStandInForATitle: a document with no title of any
// kind, under --title-append-generated-hash, was published as "- a159f529"
// instead of failing on the missing title.
func TestGeneratedHashDoesNotStandInForATitle(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\nbody\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
		TitleAppendGeneratedHash: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page title is not set")
}
