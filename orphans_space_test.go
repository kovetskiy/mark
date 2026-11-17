package mark

import (
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

	config := spaceOrphanConfig(server, dir, action)
	config.Page.Space = space

	return server, api, ids, dir, publishJSON(t, config)
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
			require.Len(t, got.Orphans, 2)
			for _, orphan := range got.Orphans {
				assert.Equal(t, report.OnOrphanReport, orphan.Action)
				assert.Equal(t,
					`space "ONE" is not the run's space, "TWO"; it is left for a run whose --space is ONE, `+
						`or to be removed by hand`, orphan.Reason)
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
// looked at when its documents spell it in another case, and with the spaces
// around it read away, as they are for the documents given it.
func TestOnOrphanActsInTheRunsOwnSpace(t *testing.T) {
	for space, header := range map[string]string{"one": "<!-- Space: ONE -->\n", " ONE ": ""} {
		t.Run(space, func(t *testing.T) {
			ctx := context.Background()
			server, api := spaceOrphanServer(t, "ONE")

			dir := t.TempDir()
			writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
			writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

			config := spaceOrphanConfig(server, dir, report.OnOrphanDelete)
			config.Page.Space = space
			require.NoError(t, Run(ctx, config))

			gone := findPageID(t, api, "ONE", "Gone")
			require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
			require.NoError(t, Run(ctx, config))

			assert.True(t, server.Page(gone).Trashed)
		})
	}
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

// movedSpaceFixture publishes keep.md and moved.md into ONE with no run
// space, then, in a run given space, moved.md with its Space header naming
// TWO. It returns the id of the page moved.md had in ONE, the directory, and
// the log and JSON report of the second run.
func movedSpaceFixture(
	t *testing.T, action report.OnOrphan, space string, dryRun bool,
) (*confluencetest.Server, *confluence.API, string, string, string, string) {
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

	config.Page.Space = space
	config.Mode.DryRun = dryRun
	var out string
	logged := captureLog(t, func() {
		out = publishJSON(t, config)
	})

	return server, api, old, dir, logged, out
}

// TestOnOrphanHandlesThePageADocumentLeftInTheRunsSpace: a document whose
// Space header now names another space left its page in the run's own space,
// where it is an orphan like any other, in the report and in a dry run.
func TestOnOrphanHandlesThePageADocumentLeftInTheRunsSpace(t *testing.T) {
	server, api, old, dir, _, out := movedSpaceFixture(t, report.OnOrphanDelete, "ONE", false)
	moved := filepath.Join(dir, "moved.md")

	assert.True(t, server.Page(old).Trashed)
	assert.False(t, trackedIn(t, api, "ONE", moved))
	assert.True(t, trackedIn(t, api, "TWO", moved))

	got := decodeReport(t, []byte(out))
	require.Len(t, got.Orphans, 1)
	assert.Equal(t, old, got.Orphans[0].PageID)
	assert.Equal(t, report.OnOrphanDelete, got.Orphans[0].Action)

	server, _, old, _, logged, _ := movedSpaceFixture(t, report.OnOrphanDelete, "ONE", true)
	assert.False(t, server.Page(old).Trashed)
	assert.Contains(t, logged, `page \"Moved\" would be deleted`)
}

// TestOnOrphanLeavesThePageADocumentLeftInAnotherSpace: in a run given the
// space the document moved to, the space it left is not the run's, so the
// page stays, tracked and reported with the reason, and a dry run says so too.
func TestOnOrphanLeavesThePageADocumentLeftInAnotherSpace(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive, report.OnOrphanReport} {
		t.Run(string(action), func(t *testing.T) {
			const warning = `space \"ONE\": 1 tracked page(s) were not published by this run and are left alone, ` +
				`still tracked, for a run whose --space is ONE or to be removed by hand, ` +
				`since space \"ONE\" is not the run's space, \"TWO\": `

			server, api, old, dir, logged, out := movedSpaceFixture(t, action, "TWO", false)
			moved := filepath.Join(dir, "moved.md")

			page := server.Page(old)
			assert.False(t, page.Trashed)
			assert.False(t, page.Archived)
			assert.True(t, trackedIn(t, api, "ONE", moved))
			assert.True(t, trackedIn(t, api, "TWO", moved))
			assert.Contains(t, logged, warning+filepath.ToSlash(moved))
			assert.NotContains(t, logged, "had no matching source file")

			got := decodeReport(t, []byte(out))
			require.Len(t, got.Orphans, 1)
			assert.Equal(t, old, got.Orphans[0].PageID)
			assert.Equal(t, report.OnOrphanReport, got.Orphans[0].Action)
			assert.Equal(t,
				`space "ONE" is not the run's space, "TWO"; it is left for a run whose --space is ONE, `+
					`or to be removed by hand`, got.Orphans[0].Reason)

			server, api, old, dir, logged, _ = movedSpaceFixture(t, action, "TWO", true)
			assert.False(t, server.Page(old).Trashed)
			assert.True(t, trackedIn(t, api, "ONE", filepath.Join(dir, "moved.md")))
			assert.Contains(t, logged, warning)
		})
	}
}

// TestOnOrphanForgetsAnEntryWhosePageWentWithItsDocument: the page was moved
// to the other space and the document's Space header followed, so the space
// it left has no page of it to remove or to report on every run, whichever
// of the two the run was given and however the header spells the new one.
func TestOnOrphanForgetsAnEntryWhosePageWentWithItsDocument(t *testing.T) {
	for _, run := range []struct{ space, header string }{
		{"", "TWO"}, {"ONE", "TWO"}, {"one", "two"}, {"TWO", "TWO"},
	} {
		t.Run("space "+run.space+" header "+run.header, func(t *testing.T) {
			ctx := context.Background()
			server := confluencetest.New(t)
			one := server.AddSpace("ONE")
			for _, key := range []string{"ONE", "TWO"} {
				server.SetHomepage(key, server.AddPage(key, "Home "+key, "page", "").ID)
			}
			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

			dir := t.TempDir()
			writeFile(t, dir, "keep.md", "<!-- Space: ONE -->\n<!-- Title: Keep -->\n\nKeep.\n")
			moved := writeFile(t, dir, "moved.md", "<!-- Space: ONE -->\n<!-- Title: Moved -->\n\nMoved.\n")

			config := spaceOrphanConfig(server, dir, report.OnOrphanDelete)
			require.NoError(t, Run(ctx, config))

			// The fake cannot move a page between spaces: the entry is pointed
			// at a page of the same title in TWO instead.
			old := findPageID(t, api, "ONE", "Moved")
			there := server.AddPage("TWO", "Moved", "page", findPageID(t, api, "TWO", "Home TWO"))
			server.DeletePage(old)
			repointEntries(t, server, one.ID,
				manifest.PropertyKey(manifest.ShardFor(filepath.ToSlash(moved))), old, there.ID)

			writeFile(t, dir, "moved.md", "<!-- Space: "+run.header+" -->\n<!-- Title: Moved -->\n\nMoved.\n")

			config.Page.Space = run.space
			out := publishJSON(t, config)

			assert.False(t, server.Page(there.ID).Trashed)
			assert.Empty(t, decodeReport(t, []byte(out)).Orphans)
			assert.Empty(t, warningsOf(t, out, report.WarningPlacement),
				"no page is left behind in the space it left")
			assert.False(t, trackedIn(t, api, "ONE", moved), "the space it left stops tracking it")

			entry, known, err := manifest.NewStore(api).Lookup(ctx, "TWO", moved)
			require.NoError(t, err)
			require.True(t, known)
			assert.Equal(t, there.ID, entry.PageID)
		})
	}
}
