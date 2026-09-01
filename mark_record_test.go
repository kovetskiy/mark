package mark

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNothingIsRecordedForAPageThatWasNotWritten: the mapping asserts what is on
// the page. Recording it before the upload, now that the manifest is saved
// however the run ends, meant a body refused as malformed still left the
// manifest claiming the new title and a fingerprint of the source that produced
// it -- and the fingerprint is what rename detection matches on, so the next run
// read a document it had never published as unchanged.
func TestNothingIsRecordedForAPageThatWasNotWritten(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// An unbalanced tag the author wrote: the well-formedness gate refuses the
	// body, so nothing is uploaded.
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Refused -->

Text with an <span>unclosed tag.
`)

	require.Error(t, Run(trackingConfig(server, file)))

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	_, ok, err := store.Lookup("DOCS", filepath.Join(dir, "doc.md"))
	require.NoError(t, err)
	assert.False(t, ok,
		"a page whose body was never uploaded must not be in the manifest")
}

// TestRecordedTitleIsTheOneThePageCarries covers the same ordering from the
// other side: a successful publish still has to record, or tracking stops
// working altogether.
func TestRecordedTitleIsTheOneThePageCarries(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Recorded"))
	require.NoError(t, Run(trackingConfig(server, file)))

	published, err := api.FindPage("DOCS", "Recorded", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup("DOCS", filepath.Join(dir, "doc.md"))
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, published.ID, entry.PageID)
	assert.Equal(t, "Recorded", entry.Title)
	assert.NotEmpty(t, entry.Hash, "the fingerprint is what a rename is matched on")
}
