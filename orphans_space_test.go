package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	return Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Output:    io.Discard,
		},
		Files: filepath.Join(dir, "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		Tracking: TrackingConfig{TrackPages: true, OnOrphan: action},
	}
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
	own := "<!-- Space: TWO -->\n"
	if space != "" {
		own = ""
	}
	writeFile(t, dir, "own.md", own+"<!-- Title: Own -->\n\nOwn.\n")
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

			var got report.Report
			require.NoError(t, json.Unmarshal([]byte(out), &got))
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

			var got report.Report
			require.NoError(t, json.Unmarshal([]byte(out), &got))
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

	var got report.Report
	require.NoError(t, json.Unmarshal([]byte(out), &got))
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
