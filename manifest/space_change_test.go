package manifest_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spaceChangeServer has keep.md and moved.md tracked in ONE, and a second
// space, TWO.
func spaceChangeServer(t *testing.T) *confluencetest.Server {
	t.Helper()

	ctx := t.Context()
	server := confluencetest.New(t)
	server.AddSpace("ONE")
	server.AddSpace("TWO")

	store := runStore(t, server, "keep.md", "moved.md")
	require.NoError(t, store.Record(ctx, "ONE", "keep.md", "1", "Keep", "k"))
	require.NoError(t, store.Record(ctx, "ONE", "moved.md", "2", "Moved", "m"))
	require.NoError(t, store.Save(ctx))

	return server
}

// TestOrphansLeaveOutADocumentRecordedInAnotherSpace: a document the run
// publishes into another space is not gone from the one it left, which stops
// tracking it once told to.
func TestOrphansLeaveOutADocumentRecordedInAnotherSpace(t *testing.T) {
	ctx := t.Context()
	server := spaceChangeServer(t)

	next := runStore(t, server, "keep.md", "moved.md")
	require.NoError(t, next.Record(ctx, "ONE", "keep.md", "1", "Keep", "k"))
	_, known, err := next.Lookup(ctx, "TWO", "moved.md")
	require.NoError(t, err)
	require.False(t, known)
	require.NoError(t, next.Record(ctx, "TWO", "moved.md", "3", "Moved", "m"))

	assert.Empty(t, next.Orphans("ONE"))
	assert.Empty(t, next.Orphans("TWO"))

	_, known, err = next.Recorded(ctx, "ONE", "moved.md")
	require.NoError(t, err)
	assert.True(t, known, "looking for orphans leaves the entry to ForgetMoved")

	next.ForgetMoved("ONE")
	next.ForgetMoved("TWO")
	require.NoError(t, next.Save(ctx))

	after := runStore(t, server, "keep.md", "moved.md")
	_, known, err = after.Recorded(ctx, "ONE", "moved.md")
	require.NoError(t, err)
	assert.False(t, known, "the space it left no longer tracks it")

	entry, known, err := after.Recorded(ctx, "TWO", "moved.md")
	require.NoError(t, err)
	require.True(t, known)
	assert.Equal(t, "3", entry.PageID)
}

// TestForgetMovedReadsASpaceKeyWithoutRegardToCase: the space a document left
// stops tracking it however the caller spells that space's key.
func TestForgetMovedReadsASpaceKeyWithoutRegardToCase(t *testing.T) {
	ctx := t.Context()
	server := spaceChangeServer(t)

	next := runStore(t, server, "keep.md", "moved.md")
	require.NoError(t, next.Record(ctx, "ONE", "keep.md", "1", "Keep", "k"))
	require.NoError(t, next.Record(ctx, "TWO", "moved.md", "3", "Moved", "m"))

	next.ForgetMoved("one")

	_, known, err := next.Recorded(ctx, "ONE", "moved.md")
	require.NoError(t, err)
	assert.False(t, known)
}

// TestOrphansKeepADocumentOnlyLookedUpInAnotherSpace: until the document is
// recorded where it now publishes, the space it left keeps its entry.
func TestOrphansKeepADocumentOnlyLookedUpInAnotherSpace(t *testing.T) {
	ctx := t.Context()
	server := spaceChangeServer(t)

	next := runStore(t, server, "keep.md", "moved.md")
	require.NoError(t, next.Record(ctx, "ONE", "keep.md", "1", "Keep", "k"))
	_, _, err := next.Lookup(ctx, "TWO", "moved.md")
	require.NoError(t, err)

	assert.Empty(t, next.Orphans("ONE"))
	next.ForgetMoved("ONE")

	entry, known, err := next.Recorded(ctx, "ONE", "moved.md")
	require.NoError(t, err)
	require.True(t, known)
	assert.Equal(t, "2", entry.PageID)
}

// TestOrphansKeepAPathTheRunDoesNotPublish: a path recorded in two spaces
// that is not one of the run's documents is still looked for in each.
func TestOrphansKeepAPathTheRunDoesNotPublish(t *testing.T) {
	ctx := t.Context()
	server := spaceChangeServer(t)

	next := runStore(t, server, "keep.md")
	require.NoError(t, next.Record(ctx, "ONE", "keep.md", "1", "Keep", "k"))
	require.NoError(t, next.Record(ctx, "TWO", "moved.md", "3", "Moved", "m"))

	next.ForgetMoved("ONE")

	assert.Equal(t, []string{"moved.md"}, next.Orphans("ONE"))
}

// TestOrphansKeepADirectoryRecordedInTwoSpaces: a directory whose documents
// publish to two spaces has a page in each. It did not change space, so the
// one whose space it no longer publishes anything to is still looked for.
func TestOrphansKeepADirectoryRecordedInTwoSpaces(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	server.AddSpace("ONE")
	server.AddSpace("TWO")

	directory := manifest.DocumentDirectoryKey("guides", "intro")
	store := runStore(t, server)
	store.SetRunDocuments("guides", []string{"a", "b", directory})
	require.NoError(t, store.Record(ctx, "ONE", "a", "1", "A", "a"))
	require.NoError(t, store.Record(ctx, "ONE", directory, "2", "Intro", ""))
	require.NoError(t, store.Record(ctx, "TWO", "b", "3", "B", "b"))
	require.NoError(t, store.Record(ctx, "TWO", directory, "4", "Intro", ""))
	require.NoError(t, store.Save(ctx))

	next := runStore(t, server)
	next.SetRunDocuments("guides", []string{"b", directory})
	_, _, err := next.Lookup(ctx, "ONE", "unrelated")
	require.NoError(t, err)
	require.NoError(t, next.Record(ctx, "TWO", "b", "3", "B", "b"))
	require.NoError(t, next.Record(ctx, "TWO", directory, "4", "Intro", ""))

	next.ForgetMoved("ONE")

	assert.Equal(t, []string{"a", directory}, next.Orphans("ONE"))
}
