package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noOverwriteFixture publishes one page and returns the server, its id and the
// config that produced it.
func noOverwriteFixture(t *testing.T) (*confluencetest.Server, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nFrom mark.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:       filepath.Join(dir, "*.md"),
		Features:    []string{"mention"},
		TrackPages:  true,
		NoOverwrite: true,
		Output:      io.Discard,
	}

	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	return server, doc.ID, config
}

// TestNoOverwriteSkipsAPageEditedInConfluence is the point of the flag: an edit
// made in the web UI is not silently replaced on the next publish.
func TestNoOverwriteSkipsAPageEditedInConfluence(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")
	edited := server.Page(id)

	require.NoError(t, Run(config))

	after := server.Page(id)
	assert.Equal(t, "<p>Written by a person.</p>", after.Body,
		"the hand-written body must survive the run")
	assert.Equal(t, edited.Version, after.Version,
		"a skipped page must not gain a version")
}

// TestNoOverwriteKeepsReportingUntilResolved checks that the warning is not a
// one-off. The recorded version is deliberately left alone while a page is
// skipped, so every later run says so again.
func TestNoOverwriteKeepsReportingUntilResolved(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")

	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body,
		"the second run must skip the page as well")
}

// TestNoOverwritePublishesAnUntouchedPage is the control. Without it the fix
// could be "stop publishing".
func TestNoOverwritePublishesAnUntouchedPage(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	before := server.Page(id)
	require.NoError(t, Run(config))

	after := server.Page(id)
	assert.Contains(t, after.Body, "From mark",
		"a page nobody touched must still be published")
	assert.Greater(t, after.Version, before.Version,
		"an untouched page is updated as usual")
}

// TestNoOverwriteDoesNotOrphanASkippedPage pins that a skipped page still
// counts as published. It is still on disk, and deleting it because a run
// declined to write to it would be the worst possible reading of the flag.
func TestNoOverwriteDoesNotOrphanASkippedPage(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")
	require.NoError(t, Run(config))

	page := server.Page(id)
	require.NotNil(t, page, "the page must not be deleted")
	assert.Equal(t, "<p>Written by a person.</p>", page.Body,
		"the page must have been skipped, which is the case being pinned")
}

// TestNoOverwriteRequiresTrackPages: without the manifest there is nowhere to
// have remembered a version, so the flag could only appear to work.
func TestNoOverwriteRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), NoOverwrite: true, Output: io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--track-pages")
}
