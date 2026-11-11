package mark

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adrScope is the scope the partial document tests publish under, as the
// manifest records it.
const adrScope = "documents://adr"

// adr is a document keyed by name and titled "Record <name>".
func adr(name string) Document {
	return Document{
		Path: name + ".md", Content: []byte(markdownWithTitle("Record " + name)), FS: fstest.MapFS{},
	}
}

// partialDocumentsFixture publishes a, b and c under the adr scope, as the
// run publishing the whole scope does, and returns the server and that run.
func partialDocumentsFixture(t *testing.T) (*confluencetest.Server, Config) {
	t.Helper()

	server, _ := docsSpace(t)
	full := documentsConfig(server, adr("a"), adr("b"), adr("c"))
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))

	return server, full
}

// partialDocumentsOf is full narrowed to documents, with keys the scope's
// other existing keys, and --on-orphan delete.
func partialDocumentsOf(full Config, keys []string, documents ...Document) Config {
	partial := full
	partial.Documents = documents
	partial.DocumentKeys = keys
	partial.Tracking.Partial = true
	partial.Tracking.OnOrphan = report.OnOrphanDelete

	return partial
}

// renameFixture publishes old-name.md and other.md under the adr scope,
// titled from their names, and returns the server, that run, the document
// maker and both pages.
func renameFixture(t *testing.T) (*confluencetest.Server, Config, func(path, body string) Document, string, string) {
	t.Helper()

	server, _ := docsSpace(t)
	doc := func(path, body string) Document {
		return Document{
			Path:    path,
			Content: []byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\n" + body + "\n"),
			FS:      fstest.MapFS{},
		}
	}

	full := documentsConfig(server, doc("old-name.md", "Renamed."), doc("other.md", "Left alone."))
	full.DocumentScope = "adr"
	full.Page.TitleFromFilename = true
	require.NoError(t, Run(context.Background(), full))

	return server, full, doc, mustFind(t, server, "Old Name"), mustFind(t, server, "Other")
}

// TestPartialDocumentsLeaveTheListedKeysAlone: a partial run publishing some
// of a scope's documents, given the rest of its keys, removes none of them
// under delete, and they stay tracked under the scope as they were.
func TestPartialDocumentsLeaveTheListedKeysAlone(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID, cID := mustFind(t, server, "Record b"), mustFind(t, server, "Record c")
	before := map[string]any{}
	for _, key := range []string{"b.md", "c.md"} {
		entry, ok := lookupKey(t, server, key)
		require.True(t, ok)
		before[key] = entry
	}

	changed := adr("a")
	changed.Content = append(changed.Content, "Changed.\n"...)
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"b.md", "c.md"}, changed)))
	})

	assert.NotContains(t, logged, "had no matching source file")
	assert.Contains(t, bodyOfPageTitled(t, server, "Record a"), "Changed.")
	for _, id := range []string{bID, cID} {
		assert.False(t, server.Page(id).Trashed)
	}
	for _, key := range []string{"b.md", "c.md"} {
		entry, ok := lookupKey(t, server, key)
		require.True(t, ok, key)
		assert.Equal(t, before[key], entry, "%s is left as it was", key)
		assert.Equal(t, adrScope, entry.Glob)
	}
}

// TestPartialDocumentsTakeAKeyLeftOutAsGone: a tracked key of the scope that
// is neither published nor listed is an orphan, and the run says how many
// keys it took as gone; reported, it stays tracked, and with delete its page
// is trashed and the key forgotten.
func TestPartialDocumentsTakeAKeyLeftOutAsGone(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID, cID := mustFind(t, server, "Record b"), mustFind(t, server, "Record c")

	reporting := partialDocumentsOf(full, []string{"c.md"}, adr("a"))
	reporting.Tracking.OnOrphan = report.OnOrphanReport
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), reporting)) })
	assert.Contains(t, logged, "had no matching source file in this run: b.md")
	assert.Contains(t, logged, "1 key(s) of documents://adr are neither published nor named in DocumentKeys")
	_, ok := lookupKey(t, server, "b.md")
	assert.True(t, ok, "only reported, so still tracked")

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"c.md"}, adr("a"))))
	assert.True(t, server.Page(bID).Trashed)
	assert.False(t, server.Page(cID).Trashed)
	_, ok = lookupKey(t, server, "b.md")
	assert.False(t, ok)
}

// TestPartialDocumentRenameIsFollowedByContent: a document under a new Key,
// its title from the new name, is taken for the one gone key with the same
// content without that key being named anywhere; the page follows it under
// the scope and nothing is left to orphan.
func TestPartialDocumentRenameIsFollowedByContent(t *testing.T) {
	server, full, doc, oldID, otherID := renameFixture(t)

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"other.md"},
			doc("new-name.md", "Renamed."))))
	})

	entry, ok := lookupKey(t, server, "new-name.md")
	require.True(t, ok)
	assert.Equal(t, oldID, entry.PageID, "the gone key's page follows the document")
	assert.Equal(t, adrScope, entry.Glob)
	assert.Equal(t, "New Name", server.Page(oldID).Title)
	assert.False(t, server.Page(oldID).Trashed)
	_, ok = lookupKey(t, server, "old-name.md")
	assert.False(t, ok, "the old key handed its entry over")
	assert.NotContains(t, logged, "had no matching source file")
	assert.Equal(t, "Other", server.Page(otherID).Title)
}

// TestPartialDocumentCopyOfAListedKeyGetsItsOwnPage: a document under a new
// Key with the content of a key that still exists is a copy, not a rename: a
// listed key is never a rename source. Left out of the list, the same key is
// gone, and the document is its rename.
func TestPartialDocumentCopyOfAListedKeyGetsItsOwnPage(t *testing.T) {
	server, full, doc, oldID, otherID := renameFixture(t)

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"old-name.md", "other.md"},
		doc("copy.md", "Left alone."))))

	assert.Equal(t, "Other", server.Page(otherID).Title)
	otherEntry, ok := lookupKey(t, server, "other.md")
	require.True(t, ok)
	assert.Equal(t, otherID, otherEntry.PageID)
	copyEntry, ok := lookupKey(t, server, "copy.md")
	require.True(t, ok)
	assert.NotEqual(t, otherID, copyEntry.PageID)
	assert.False(t, server.Page(oldID).Trashed)

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full,
		[]string{"old-name.md", "other.md", "copy.md"}, doc("second-copy.md", "Renamed."))))
	assert.Equal(t, oldID, mustFind(t, server, "Old Name"), "a listed key is never a rename source")

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full,
		[]string{"other.md", "copy.md", "second-copy.md"}, doc("renamed.md", "Renamed."))))
	entry, ok := lookupKey(t, server, "renamed.md")
	require.True(t, ok)
	assert.Equal(t, oldID, entry.PageID, "left out of the list, old-name.md is gone and followed")
}

// TestPartialDocumentMoveFollowsAGoneKey: a document whose Key changed and
// whose title did not finds its page by the title. From a gone key the entry
// moves to the new key under the scope; from a listed key it is two
// documents claiming one page, which is warned about.
func TestPartialDocumentMoveFollowsAGoneKey(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID := mustFind(t, server, "Record b")

	moved := adr("b")
	moved.Path = "moved/b.md"
	moved.Content = append(moved.Content, "Moved.\n"...)

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"a.md", "b.md", "c.md"}, moved)))
	})
	assert.Contains(t, logged, "both publish to page")
	_, ok := lookupKey(t, server, "b.md")
	assert.True(t, ok, "a listed key keeps its entry")

	logged = captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"a.md", "c.md"}, moved)))
	})
	assert.NotContains(t, logged, "had no matching source file")

	entry, ok := lookupKey(t, server, "moved/b.md")
	require.True(t, ok)
	assert.Equal(t, bID, entry.PageID)
	assert.Equal(t, adrScope, entry.Glob)
	_, ok = lookupKey(t, server, "b.md")
	assert.False(t, ok)
	assert.False(t, server.Page(bID).Trashed)
}

// TestPartialDocumentsKeepTheScopeWhenEveryKeyIsGone: a key list naming no
// tracked key, beside a document new to the scope, reads every tracked key as
// gone at once, as a bad list would; they are reported, not removed.
func TestPartialDocumentsKeepTheScopeWhenEveryKeyIsGone(t *testing.T) {
	server, full := partialDocumentsFixture(t)

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"typo.md"}, adr("d"))))
	})

	assert.Contains(t, logged, "3 key(s) of documents://adr are neither published nor named")
	assert.Contains(t, logged, "a failed render or a bad key list")
	for _, name := range []string{"a", "b", "c"} {
		assert.False(t, server.Page(mustFind(t, server, "Record "+name)).Trashed, name)
		_, ok := lookupKey(t, server, name+".md")
		assert.True(t, ok, name)
	}
}

// TestPartialDocumentsWithoutKeysCoverOnlyTheirOwn: without DocumentKeys a
// partial run knows only the keys it publishes, so it finds no orphans and
// follows no rename or move, and says which keys would let it follow a move.
func TestPartialDocumentsWithoutKeysCoverOnlyTheirOwn(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID, cID := mustFind(t, server, "Record b"), mustFind(t, server, "Record c")

	moved := adr("b")
	moved.Path = "moved/b.md"
	moved.Content = append(moved.Content, "Moved.\n"...)
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, nil, adr("a"), moved)))
	})

	assert.NotContains(t, logged, "had no matching source file")
	assert.Contains(t, logged, "name every key of its scope that still exists to follow the move")
	assert.False(t, server.Page(bID).Trashed)
	assert.False(t, server.Page(cID).Trashed)
	for _, key := range []string{"b.md", "c.md"} {
		entry, ok := lookupKey(t, server, key)
		require.True(t, ok, key)
		assert.Equal(t, adrScope, entry.Glob)
	}

	renames, renameRun, doc, _, otherID := renameFixture(t)
	require.NoError(t, Run(context.Background(), partialDocumentsOf(renameRun, nil, doc("renamed.md", "Left alone."))))
	assert.Equal(t, "Other", renames.Page(otherID).Title, "no rename without the scope's keys")
}

// TestFullScopedRunAfterPartialRuns: what partial runs left tracked is still
// the scope's, and the run publishing the whole scope deals with a document
// gone from it.
func TestFullScopedRunAfterPartialRuns(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	cID := mustFind(t, server, "Record c")

	reporting := partialDocumentsOf(full, []string{"b.md"}, adr("a"), adr("d"))
	reporting.Tracking.OnOrphan = report.OnOrphanReport
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), reporting)) })
	assert.Contains(t, logged, `had no matching source file in this run: c.md"}`)

	full.Documents = []Document{adr("a"), adr("b"), adr("d")}
	full.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), full))

	assert.True(t, server.Page(cID).Trashed)
	for _, title := range []string{"Record a", "Record b", "Record d"} {
		assert.False(t, server.Page(mustFind(t, server, title)).Trashed, title)
	}
	entry, ok := lookupKey(t, server, "d.md")
	require.True(t, ok)
	assert.Equal(t, adrScope, entry.Glob, "a document a partial run added is the scope's")
}

// TestDocumentKeysNeedPartialAndAScope: without Partial a run's documents
// are its whole scope, and would remove those left out; without a scope no
// key is covered. Both are refused before publishing, and so are keys with
// nothing to publish, which name no space to look in.
func TestDocumentKeysNeedPartialAndAScope(t *testing.T) {
	server, full := partialDocumentsFixture(t)

	notPartial := partialDocumentsOf(full, []string{"b.md"}, adr("a"))
	notPartial.Tracking.Partial = false
	unscoped := partialDocumentsOf(full, []string{"b.md"}, adr("a"))
	unscoped.DocumentScope = ""

	for name, config := range map[string]Config{"not partial": notPartial, "no scope": unscoped} {
		err := Run(context.Background(), config)
		require.ErrorContains(t, err, "DocumentKeys requires Tracking.Partial and a DocumentScope", name)
	}

	removalsOnly := partialDocumentsOf(full, []string{"a.md"})
	require.ErrorContains(t, Run(context.Background(), removalsOnly), "finds no space to look for the scope's pages in")
	for _, name := range []string{"b", "c"} {
		assert.False(t, server.Page(mustFind(t, server, "Record "+name)).Trashed, name)
	}
}

// TestPartialUnscopedDocumentsSayItDoesNothing: documents with no scope take
// part in no orphan handling, partial or not, which is warned about.
func TestPartialUnscopedDocumentsSayItDoesNothing(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, adr("a"), adr("b"))
	require.NoError(t, Run(context.Background(), config))

	config.Documents = []Document{adr("a")}
	config.Tracking.Partial = true
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), config)) })

	assert.Contains(t, logged, "Tracking.Partial does nothing for Documents without a DocumentScope")
	assert.NotContains(t, logged, "had no matching source file")
	_, ok := lookupKey(t, server, "b.md")
	assert.True(t, ok)
}

// TestPartialRunOfFilesAndDocuments: in one partial run files follow the
// paths it names and documents the keys it lists, each removing only what is
// gone, though the documents published nothing in the space.
func TestPartialRunOfFilesAndDocuments(t *testing.T) {
	server, files, dir := partialFixture(t)
	full := files
	full.Documents = []Document{adr("x"), adr("y"), adr("z")}
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))

	bID, cID := pageIDTitled(t, server, "Page b"), pageIDTitled(t, server, "Page c")
	xID, yID, zID := mustFind(t, server, "Record x"), mustFind(t, server, "Record y"), mustFind(t, server, "Record z")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	partial := pathsOf(full, dir, "a.md", "b.md")
	partial.Documents = nil
	partial.DocumentKeys = []string{"x.md", "z.md"}
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), partial)) })

	assert.True(t, server.Page(bID).Trashed, "b.md was named and is gone")
	assert.False(t, server.Page(cID).Trashed, "c.md was not named")
	assert.True(t, server.Page(yID).Trashed, "y.md is in neither list")
	assert.False(t, server.Page(xID).Trashed, "x.md was listed")
	assert.False(t, server.Page(zID).Trashed, "z.md was listed")
	assert.NotContains(t, logged, "is left alone")
}
