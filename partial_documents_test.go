package mark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// partialDocumentsOf is full narrowed to documents and the keys it names,
// with --on-orphan delete.
func partialDocumentsOf(full Config, keys []string, documents ...Document) Config {
	partial := full
	partial.Documents = documents
	partial.DocumentKeys = keys
	partial.Tracking.Partial = true
	partial.Tracking.OnOrphan = report.OnOrphanDelete

	return partial
}

// TestPartialDocumentsLeaveTheRestOfTheScopeAlone: a partial run publishing
// some of a scope's documents removes none of the others, and they stay
// tracked under the scope.
func TestPartialDocumentsLeaveTheRestOfTheScopeAlone(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID, cID := mustFind(t, server, "Record b"), mustFind(t, server, "Record c")

	changed := adr("a")
	changed.Content = append(changed.Content, "Changed.\n"...)
	require.NoError(t, Run(context.Background(), partialDocumentsOf(full, nil, changed)))

	assert.Contains(t, bodyOfPageTitled(t, server, "Record a"), "Changed.")
	for _, id := range []string{bID, cID} {
		assert.False(t, server.Page(id).Trashed)
	}
	for _, key := range []string{"b.md", "c.md"} {
		entry, ok := lookupKey(t, server, key)
		require.True(t, ok, key)
		assert.Equal(t, adrScope, entry.Glob)
	}
}

// TestPartialDocumentsActOnTheKeysTheyName: a key named in DocumentKeys and
// not published is an orphan; reported, it stays tracked for the run
// publishing the whole scope, and with delete its page is trashed.
func TestPartialDocumentsActOnTheKeysTheyName(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID, cID := mustFind(t, server, "Record b"), mustFind(t, server, "Record c")

	reporting := partialDocumentsOf(full, []string{"b.md"}, adr("a"))
	reporting.Tracking.OnOrphan = report.OnOrphanReport
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), reporting)) })
	assert.Contains(t, logged, "had no matching source file in this run: b.md")
	_, ok := lookupKey(t, server, "b.md")
	assert.True(t, ok, "only reported, so still tracked")

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"b.md"}, adr("a"))))
	assert.True(t, server.Page(bID).Trashed)
	assert.False(t, server.Page(cID).Trashed)
	_, ok = lookupKey(t, server, "b.md")
	assert.False(t, ok)
}

// TestPartialDocumentRenameIsFollowedFromANamedKey: a document under a new
// Key, its title from the new name, is taken for the named key with the same
// content, and not for a key the run does not name.
func TestPartialDocumentRenameIsFollowedFromANamedKey(t *testing.T) {
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
	oldID, otherID := mustFind(t, server, "Old Name"), mustFind(t, server, "Other")

	require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"old-name.md"},
		doc("new-name.md", "Renamed."), doc("copy.md", "Left alone."))))

	entry, ok := lookupKey(t, server, "new-name.md")
	require.True(t, ok)
	assert.Equal(t, oldID, entry.PageID, "the named key's page follows the document")
	assert.Equal(t, adrScope, entry.Glob)
	assert.Equal(t, "New Name", server.Page(oldID).Title)
	_, ok = lookupKey(t, server, "old-name.md")
	assert.False(t, ok, "the old key handed its entry over")

	assert.Equal(t, "Other", server.Page(otherID).Title, "other.md was not named")
	copyEntry, ok := lookupKey(t, server, "copy.md")
	require.True(t, ok)
	assert.NotEqual(t, otherID, copyEntry.PageID)
}

// TestPartialDocumentMoveKeepsItsScope: a document whose Key changed and
// whose title did not finds its page by the title; with its old key named the
// entry moves to the new key under the scope, and without it the run says
// which key to name.
func TestPartialDocumentMoveKeepsItsScope(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	bID := mustFind(t, server, "Record b")

	moved := adr("b")
	moved.Path = "moved/b.md"

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, nil, moved)))
	})
	assert.Contains(t, logged, "name the key b.md beside the documents to follow the move")

	logged = captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialDocumentsOf(full, []string{"b.md"}, moved)))
	})
	assert.NotContains(t, logged, "both publish to page")

	entry, ok := lookupKey(t, server, "moved/b.md")
	require.True(t, ok)
	assert.Equal(t, bID, entry.PageID)
	assert.Equal(t, adrScope, entry.Glob)
	_, ok = lookupKey(t, server, "b.md")
	assert.False(t, ok)
	assert.False(t, server.Page(bID).Trashed)
}

// TestFullScopedRunAfterPartialRuns: what partial runs left tracked is still
// the scope's, and the run publishing the whole scope deals with a document
// gone from it.
func TestFullScopedRunAfterPartialRuns(t *testing.T) {
	server, full := partialDocumentsFixture(t)
	cID := mustFind(t, server, "Record c")

	reporting := partialDocumentsOf(full, []string{"c.md"}, adr("a"), adr("d"))
	reporting.Tracking.OnOrphan = report.OnOrphanReport
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), reporting)) })
	assert.Contains(t, logged, `had no matching source file in this run: c.md"}`,
		"only the named key, not b.md")

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

// TestDocumentKeysNeedPartialAndAScope: without Partial a run covers every
// key of its scope, and would remove the documents left out; without a scope
// no key is covered. Both are refused before publishing.
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

	// Removals alone publish nothing, so no space is looked in.
	removalsOnly := partialDocumentsOf(full, []string{"b.md"})
	require.ErrorContains(t, Run(context.Background(), removalsOnly), "finds no space to look for them in")
	assert.False(t, server.Page(mustFind(t, server, "Record b")).Trashed)
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
// paths it names and documents the keys it names, each removing only what it
// was told to, though the documents published nothing in the space.
func TestPartialRunOfFilesAndDocuments(t *testing.T) {
	server, files, dir := partialFixture(t)
	full := files
	full.Documents = []Document{adr("x"), adr("y"), adr("z")}
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))

	bID, cID := pageIDTitled(t, server, "Page b"), pageIDTitled(t, server, "Page c")
	yID, zID := mustFind(t, server, "Record y"), mustFind(t, server, "Record z")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	partial := pathsOf(full, dir, "a.md", "b.md")
	partial.Documents = nil
	partial.DocumentKeys = []string{"y.md"}
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), partial)) })

	assert.True(t, server.Page(bID).Trashed, "b.md was named and is gone")
	assert.False(t, server.Page(cID).Trashed, "c.md was not named")
	assert.True(t, server.Page(yID).Trashed, "y.md was named and not published")
	assert.False(t, server.Page(zID).Trashed, "z.md was not named")
	assert.False(t, strings.Contains(logged, "is left alone"), logged)
}
