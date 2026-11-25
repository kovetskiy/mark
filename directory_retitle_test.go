package mark

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retitledDirectoryFixture publishes guides/setup.md and keep.md with the
// guides directory titled by a .pages file, tracked and with orphans deleted,
// and returns the ids of the directory's page and of Setup. A prefix of
// "/ex/confluence/cloud-id" has the fake answer as Cloud's gateway, on v2.
func retitledDirectoryFixture(t *testing.T, prefix string) (*confluencetest.Server, Config, string, string, string) {
	t.Helper()

	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/.pages", "title: Guides\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")
	writeAt(t, dir, "keep.md", space+"<!-- Title: Keep -->\n\nKeep.\n")

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.Connection.BaseURL = server.URL + prefix
	config.ParentsFromPath = true
	config.ParentsFromPathRoot = dir
	config.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(t.Context(), config))

	guides, setup := pageIDTitled(t, server, "Guides"), pageIDTitled(t, server, "Setup")
	require.NotEmpty(t, guides)
	require.Equal(t, guides, server.Page(setup).ParentID)

	return server, config, dir, guides, setup
}

// TestRetitledDirectoryPageIsRenamedInPlace: a directory with no document of
// its own whose title changes keeps its page, renamed, as a document's page
// is. A second page under the new title would leave the old one empty, and
// with the directory's entry pointing at the new one, out of --on-orphan's
// reach for good.
func TestRetitledDirectoryPageIsRenamedInPlace(t *testing.T) {
	for name, prefix := range map[string]string{"v1": "", "gateway": "/ex/confluence/cloud-id"} {
		t.Run(name, func(t *testing.T) {
			server, config, dir, guides, setup := retitledDirectoryFixture(t, prefix)
			server.EditPage(guides, "<p>written by hand</p>")

			writeAt(t, dir, "guides/.pages", "title: Handbook\n")
			require.NoError(t, Run(t.Context(), config))

			renamed := server.Page(guides)
			assert.Equal(t, "Handbook", renamed.Title, "the directory's page is renamed")
			assert.Equal(t, "<p>written by hand</p>", renamed.Body, "and keeps its body")
			assert.Equal(t, guides, pageIDTitled(t, server, "Handbook"), "and no second page is made")
			assert.Empty(t, pageIDTitled(t, server, "Guides"))
			assert.Equal(t, guides, server.Page(setup).ParentID, "the document stays where it was")

			entry, ok := lookupKey(t, server, filepath.Join(dir, "guides"))
			require.True(t, ok)
			assert.Equal(t, guides, entry.PageID, "the directory is still recorded against its page")
			assert.Equal(t, "Handbook", entry.Title, "under its new title")

			require.NoError(t, os.RemoveAll(filepath.Join(dir, "guides")))
			require.NoError(t, Run(t.Context(), config))
			require.NoError(t, Run(t.Context(), config))
			assert.True(t, server.Page(setup).Trashed)
			assert.True(t, server.Page(guides).Trashed, "the directory's page is an orphan once it is gone")
		})
	}
}

// TestRetitledDirectoryPageDryRunAgrees: a dry run says what the real run
// does, which is to leave the document where it is under the renamed page.
func TestRetitledDirectoryPageDryRunAgrees(t *testing.T) {
	server, config, dir, guides, setup := retitledDirectoryFixture(t, "")
	config.Update.ChangesOnly = true
	// Leaves the fingerprints --changes-only compares against.
	require.NoError(t, Run(t.Context(), config))

	writeAt(t, dir, "guides/.pages", "title: Handbook\n")
	before := len(nonGET(server))

	parsed := decodeReport(t, []byte(previewOf(t, config, report.FormatJSON)))
	assert.Len(t, nonGET(server), before, "a dry run must not write")
	assert.Equal(t, "Guides", server.Page(guides).Title)

	statuses := map[string]report.Status{}
	for _, pg := range parsed.Pages {
		statuses[pg.Title] = pg.Status
	}
	assert.Equal(t, report.StatusUnchanged, statuses["Setup"], "Setup is neither moved nor written")

	config.Report.OutputFormat = ""
	config.Output = &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), config))
	assert.Equal(t, "Handbook", server.Page(guides).Title)
	assert.Equal(t, guides, server.Page(setup).ParentID)
	for _, w := range nonGET(server)[before:] {
		assert.NotContains(t, w, "/content/"+setup, "the real run does not write Setup either")
	}
}

// TestRetitledDirectoryPageKeepsItsPageWhenTheTitleIsTaken: renaming onto a
// title another page carries is impossible, so the directory keeps the page
// it has, recorded, and is still removed with it once it goes.
func TestRetitledDirectoryPageKeepsItsPageWhenTheTitleIsTaken(t *testing.T) {
	server, config, dir, guides, setup := retitledDirectoryFixture(t, "")
	taken := server.AddPage("DOCS", "Handbook", "page", pageIDTitled(t, server, "Home"))

	writeAt(t, dir, "guides/.pages", "title: Handbook\n")
	var out bytes.Buffer
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out
	require.NoError(t, Run(t.Context(), config))

	assert.Equal(t, "Guides", server.Page(guides).Title)
	assert.Equal(t, guides, server.Page(setup).ParentID, "the document stays under the directory's page")
	assert.Contains(t, out.String(), `already carries the title`, "and the run says why")

	entry, ok := lookupKey(t, server, filepath.Join(dir, "guides"))
	require.True(t, ok)
	assert.Equal(t, guides, entry.PageID)

	// Once the title is free, the next run takes it.
	server.RenamePage(taken.ID, "Elsewhere")
	config.Report.OutputFormat = ""
	config.Output = &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), config))
	assert.Equal(t, "Handbook", server.Page(guides).Title)

	require.NoError(t, os.RemoveAll(filepath.Join(dir, "guides")))
	require.NoError(t, Run(t.Context(), config))
	require.NoError(t, Run(t.Context(), config))
	assert.True(t, server.Page(guides).Trashed, "the directory's page is an orphan once it is gone")
	assert.False(t, server.Page(taken.ID).Trashed)
}

// TestDirectoryPageRenamedByHandKeepsItsTitle: a page renamed in Confluence
// is followed, not renamed back, while the directory's own title is as it
// was.
func TestDirectoryPageRenamedByHandKeepsItsTitle(t *testing.T) {
	server, config, _, guides, setup := retitledDirectoryFixture(t, "")

	server.RenamePage(guides, "Old guides")
	for range 3 {
		require.NoError(t, Run(t.Context(), config))
		assert.Equal(t, "Old guides", server.Page(guides).Title)
		assert.Equal(t, guides, server.Page(setup).ParentID)
		assert.Empty(t, pageIDTitled(t, server, "Guides"))
	}
}

// TestHashedDirectoryPageFollowsAChangedParent: a hashed directory title
// changes with the --parents above it. The directory keeps its page, renamed
// and moved under the new parent, rather than a new page there leaving the
// old one behind.
func TestHashedDirectoryPageFollowsAChangedParent(t *testing.T) {
	server := hierarchyServer(t)
	home := pageIDTitled(t, server, "Home")
	old := server.AddPage("DOCS", "Old", "page", home)
	updated := server.AddPage("DOCS", "New", "page", home)

	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.ParentsFromPath = true
	config.ParentsFromPathRoot = dir
	config.Page.TitleAppendGeneratedHash = true
	config.Page.Parents = []string{"Old"}
	require.NoError(t, Run(t.Context(), config))

	directories := server.ChildOrder(old.ID)
	require.Len(t, directories, 1)
	guides := directories[0]
	before := server.Page(guides).Title
	require.True(t, strings.HasPrefix(before, "Guides - "), before)
	setup := server.ChildOrder(guides)
	require.Len(t, setup, 1)

	config.Page.Parents = []string{"New"}
	require.NoError(t, Run(t.Context(), config))

	after := server.Page(guides).Title
	assert.True(t, strings.HasPrefix(after, "Guides - "), after)
	assert.NotEqual(t, before, after, "the directory's page is renamed")
	assert.Equal(t, []string{guides}, server.ChildOrder(updated.ID), "and moved under New")
	assert.Empty(t, server.ChildOrder(old.ID), "and nothing is left under Old")
	assert.Equal(t, setup, server.ChildOrder(guides), "the document stays under it")
}

// TestDirectoryTitleChangedBackRenamesThePageBack: the chain under the
// first title is still recorded for the page, which refreshStaleParents
// follows to the second title; the directory's own title wins.
func TestDirectoryTitleChangedBackRenamesThePageBack(t *testing.T) {
	server, config, dir, guides, setup := retitledDirectoryFixture(t, "")

	for _, title := range []string{"Handbook", "Guides"} {
		writeAt(t, dir, "guides/.pages", "title: "+title+"\n")
		require.NoError(t, Run(t.Context(), config))
		assert.Equal(t, title, server.Page(guides).Title)
		assert.Equal(t, guides, server.Page(setup).ParentID)
	}
}
