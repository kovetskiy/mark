package mark

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spaceOrphanConfig(server *confluencetest.Server, dir string, action report.OnOrphan) Config {
	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = action

	return config
}

func spaceOrphanServer(t *testing.T, spaces ...string) (*confluencetest.Server, *confluence.API) {
	t.Helper()

	server := confluencetest.New(t)
	for _, space := range spaces {
		home := server.AddPage(space, "Home "+space, "page", "")
		server.SetHomepage(space, home.ID)
	}

	return server, confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
}

func findPageID(t *testing.T, api *confluence.API, space, title string) string {
	t.Helper()

	found, err := api.FindPage(context.Background(), space, title, "page")
	require.NoError(t, err)
	require.NotNil(t, found, "page %q in %s", title, space)

	return found.ID
}

func trackedIn(t *testing.T, api *confluence.API, space, path string) bool {
	t.Helper()

	_, known, err := manifest.NewStore(api).Lookup(context.Background(), space, path)
	require.NoError(t, err)

	return known
}

// otherSpaceFixture publishes two documents into space ONE with one --files
// pattern, then runs the same pattern, with action, over a checkout that holds
// neither: one document in the run's own space, TWO, and one whose Space
// header names ONE. It returns the ids of the two pages first published, the
// directory, and the JSON report of the second run.
func otherSpaceFixture(
	t *testing.T, action report.OnOrphan, space string,
) (*confluencetest.Server, *confluence.API, []string, string, string) {
	t.Helper()

	ctx := context.Background()
	server, api := spaceOrphanServer(t, "ONE", "TWO")

	dir := t.TempDir()
	writeFile(t, dir, "first.md", "<!-- Space: ONE -->\n<!-- Title: First -->\n\nFirst.\n")
	writeFile(t, dir, "second.md", "<!-- Space: ONE -->\n<!-- Title: Second -->\n\nSecond.\n")
	require.NoError(t, Run(ctx, spaceOrphanConfig(server, dir, action)))

	ids := []string{findPageID(t, api, "ONE", "First"), findPageID(t, api, "ONE", "Second")}

	require.NoError(t, os.Remove(filepath.Join(dir, "first.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "second.md")))
	writeFile(t, dir, "own.md", "<!-- Space: TWO -->\n<!-- Title: Own -->\n\nOwn.\n")
	writeFile(t, dir, "note.md", "<!-- Space: ONE -->\n<!-- Title: Note -->\n\nNote.\n")

	var out bytes.Buffer
	config := spaceOrphanConfig(server, dir, action)
	config.Page.Space = space
	config.Output = &out
	config.Report.OutputFormat = report.FormatJSON
	require.NoError(t, Run(ctx, config))

	return server, api, ids, dir, out.String()
}

// TestOnOrphanLeavesASpaceOnlyAHeaderNames: a run given --space looks for
// orphans in that space. A space only a document's Space header brought into
// the run keeps its tracked pages, and their entries.
func TestOnOrphanLeavesASpaceOnlyAHeaderNames(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive, report.OnOrphanReport} {
		t.Run(string(action), func(t *testing.T) {
			server, api, ids, dir, out := otherSpaceFixture(t, action, "TWO")

			for _, id := range ids {
				page := server.Page(id)
				require.NotNil(t, page)
				assert.False(t, page.Trashed, "a page in a space the run was not given is left alone")
				assert.False(t, page.Archived)
			}

			assert.True(t, trackedIn(t, api, "ONE", filepath.Join(dir, "first.md")), "and stays tracked")
			assert.True(t, trackedIn(t, api, "ONE", filepath.Join(dir, "second.md")))
			assert.True(t, trackedIn(t, api, "ONE", filepath.Join(dir, "note.md")),
				"the document itself is tracked where it published")

			got := decodeReport(t, []byte(out))
			if action == report.OnOrphanReport {
				assert.Empty(t, got.Orphans)
				return
			}

			require.Len(t, got.Orphans, 2)
			for _, orphan := range got.Orphans {
				assert.Equal(t, report.OnOrphanReport, orphan.Action)
				assert.Equal(t, `space "ONE" is not the run's space, "TWO"`, orphan.Reason)
			}
		})
	}
}

// TestOnOrphanKeepsAnEntryOfAPathTheRunPublishesInItsOwnSpace: two runs
// publish one tree, a space each. A document whose Space header names the
// other run's space brings that space into this run, where every other path
// is recorded too; those entries are the other run's and stay.
func TestOnOrphanKeepsAnEntryOfAPathTheRunPublishesInItsOwnSpace(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive, report.OnOrphanReport} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			server, api := spaceOrphanServer(t, "ONE", "TWO")

			dir := t.TempDir()
			writeFile(t, dir, "index.md", "<!-- Title: Index -->\n\nIndex.\n")
			writeFile(t, dir, "note.md", "<!-- Title: Note -->\n\nNote.\n")

			one := spaceOrphanConfig(server, dir, action)
			one.Page.Space = "ONE"
			require.NoError(t, Run(ctx, one))
			index := findPageID(t, api, "ONE", "Index")

			writeFile(t, dir, "note.md", "<!-- Space: ONE -->\n<!-- Title: Note -->\n\nNote.\n")
			two := spaceOrphanConfig(server, dir, action)
			two.Page.Space = "TWO"
			require.NoError(t, Run(ctx, two))

			page := server.Page(index)
			assert.False(t, page.Trashed)
			assert.False(t, page.Archived)

			entry, known, err := manifest.NewStore(api).Lookup(ctx, "ONE", filepath.Join(dir, "index.md"))
			require.NoError(t, err)
			require.True(t, known, "the other run's entry stays")
			assert.Equal(t, index, entry.PageID)
			assert.True(t, trackedIn(t, api, "TWO", filepath.Join(dir, "index.md")))
		})
	}
}

// TestOnOrphanActsInTheRunsOwnSpace: the space the run was given is still
// looked at, whatever case it is written in.
func TestOnOrphanActsInTheRunsOwnSpace(t *testing.T) {
	ctx := context.Background()
	server, api := spaceOrphanServer(t, "ONE")

	dir := t.TempDir()
	writeFile(t, dir, "keep.md", "<!-- Space: ONE -->\n<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", "<!-- Space: ONE -->\n<!-- Title: Gone -->\n\nGone.\n")

	config := spaceOrphanConfig(server, dir, report.OnOrphanDelete)
	config.Page.Space = "one"
	require.NoError(t, Run(ctx, config))

	gone := findPageID(t, api, "ONE", "Gone")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(ctx, config))

	assert.True(t, server.Page(gone).Trashed)
}

// TestOnOrphanActsInEverySpaceWithoutARunSpace: a run given no space takes
// every space from its documents, and looks for orphans in each.
func TestOnOrphanActsInEverySpaceWithoutARunSpace(t *testing.T) {
	server, api, ids, dir, _ := otherSpaceFixture(t, report.OnOrphanDelete, "")

	for _, id := range ids {
		assert.True(t, server.Page(id).Trashed)
	}
	assert.False(t, trackedIn(t, api, "ONE", filepath.Join(dir, "first.md")))
}

// movedSpaceFixture publishes keep.md and moved.md into ONE, then moved.md
// with its Space header naming TWO.
func movedSpaceFixture(
	t *testing.T, action report.OnOrphan, dryRun bool,
) (*confluencetest.Server, *confluence.API, string, string, string) {
	t.Helper()

	ctx := context.Background()
	server, api := spaceOrphanServer(t, "ONE", "TWO")

	dir := t.TempDir()
	writeFile(t, dir, "keep.md", "<!-- Space: ONE -->\n<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "moved.md", "<!-- Space: ONE -->\n<!-- Title: Moved -->\n\nMoved.\n")

	config := spaceOrphanConfig(server, dir, action)
	require.NoError(t, Run(ctx, config))

	old := findPageID(t, api, "ONE", "Moved")

	writeFile(t, dir, "moved.md", "<!-- Space: TWO -->\n<!-- Title: Moved -->\n\nMoved.\n")

	var out bytes.Buffer
	config.Output = &out
	config.Report.OutputFormat = report.FormatJSON
	config.Mode.DryRun = dryRun
	require.NoError(t, Run(ctx, config))

	return server, api, old, dir, out.String()
}

// TestOnOrphanLeavesThePageOfADocumentThatChangedSpace: a document whose Space
// header changed is still there, so the page it published before is left
// behind, not removed as the page of a file that is gone.
func TestOnOrphanLeavesThePageOfADocumentThatChangedSpace(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive, report.OnOrphanReport} {
		t.Run(string(action), func(t *testing.T) {
			server, api, old, dir, out := movedSpaceFixture(t, action, false)

			page := server.Page(old)
			require.NotNil(t, page)
			assert.False(t, page.Trashed, "the page left behind is not removed")
			assert.False(t, page.Archived)

			assert.NotEqual(t, old, findPageID(t, api, "TWO", "Moved"))

			got := decodeReport(t, []byte(out))
			assert.Empty(t, got.Orphans, "its file is not gone")

			moved := filepath.Join(dir, "moved.md")
			assert.True(t, trackedIn(t, api, "TWO", moved))
			assert.False(t, trackedIn(t, api, "ONE", moved), "the space it left stops tracking it")

			// And the next run has nothing more to say about it.
			config := spaceOrphanConfig(server, dir, action)
			require.NoError(t, Run(context.Background(), config))
			assert.False(t, server.Page(old).Trashed)
		})
	}
}

// TestOnOrphanDryRunLeavesTheEntryOfADocumentThatChangedSpace: a dry run
// reports no orphan for it either, and writes nothing.
func TestOnOrphanDryRunLeavesTheEntryOfADocumentThatChangedSpace(t *testing.T) {
	server, api, old, dir, out := movedSpaceFixture(t, report.OnOrphanDelete, true)

	assert.False(t, server.Page(old).Trashed)
	assert.True(t, trackedIn(t, api, "ONE", filepath.Join(dir, "moved.md")))

	got := decodeReport(t, []byte(out))
	assert.Empty(t, got.Orphans)
}

// TestOnOrphanStillRemovesAFileGoneBesideOneThatChangedSpace: only the
// document that is still there is passed over.
func TestOnOrphanStillRemovesAFileGoneBesideOneThatChangedSpace(t *testing.T) {
	ctx := context.Background()
	server, api := spaceOrphanServer(t, "ONE", "TWO")

	dir := t.TempDir()
	writeFile(t, dir, "keep.md", "<!-- Space: ONE -->\n<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "moved.md", "<!-- Space: ONE -->\n<!-- Title: Moved -->\n\nMoved.\n")
	writeFile(t, dir, "gone.md", "<!-- Space: ONE -->\n<!-- Title: Gone -->\n\nGone.\n")

	config := spaceOrphanConfig(server, dir, report.OnOrphanDelete)
	require.NoError(t, Run(ctx, config))

	old, gone := findPageID(t, api, "ONE", "Moved"), findPageID(t, api, "ONE", "Gone")

	writeFile(t, dir, "moved.md", "<!-- Space: TWO -->\n<!-- Title: Moved -->\n\nMoved.\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(ctx, config))

	assert.False(t, server.Page(old).Trashed)
	assert.True(t, server.Page(gone).Trashed)
}
