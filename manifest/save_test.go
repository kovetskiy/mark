package manifest_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sameShard returns a path other than path that is kept in the same shard, so
// two runs recording one each write the same property.
func sameShard(t *testing.T, path string) string {
	t.Helper()
	for i := range 10000 {
		candidate := fmt.Sprintf("page-%d.md", i)
		if candidate != path && manifest.ShardFor(candidate) == manifest.ShardFor(path) {
			return candidate
		}
	}
	t.Fatalf("no path shares a shard with %q", path)
	return ""
}

// otherShard returns a path kept in a different shard from path.
func otherShard(t *testing.T, path string) string {
	t.Helper()
	for i := range 10000 {
		candidate := fmt.Sprintf("page-%d.md", i)
		if manifest.ShardFor(candidate) != manifest.ShardFor(path) {
			return candidate
		}
	}
	t.Fatalf("no path is kept apart from %q", path)
	return ""
}

// savedVersions reads every shard held by owner and returns the page version
// recorded for each path.
func savedVersions(t *testing.T, server *confluencetest.Server, ownerID string) map[string]int64 {
	t.Helper()
	all := map[string]int64{}
	for i := range manifest.ShardCount {
		property := server.SpaceProperty(ownerID, manifest.PropertyKey(i))
		if property == nil {
			continue
		}
		var doc struct {
			Pages map[string]manifest.Entry `json:"pages"`
		}
		require.NoError(t, json.Unmarshal(property.Value, &doc))
		for path, entry := range doc.Pages {
			all[path] = entry.Version
		}
	}
	return all
}

// failWrites answers every property write whose path ends in one of the
// property ids with status.
func failWrites(server *confluencetest.Server, status int, body string, propertyIDs ...string) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPut {
			return 0, "", false
		}
		for _, id := range propertyIDs {
			if strings.HasSuffix(r.URL.Path, "/properties/"+id) {
				return status, body, true
			}
		}
		return 0, "", false
	})
}

// TestSaveKeepsBothRunsMappingsOfOneShard: two runs that overlap and record
// different documents kept in one shard. The second to save reads what the
// first wrote and saves its own entries over it, so both documents stay
// tracked, each with the page version its run wrote.
func TestSaveKeepsBothRunsMappingsOfOneShard(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")
	other := sameShard(t, "a.md")

	require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, first.RecordVersion(ctx, "DOCS", "a.md", 1))
	require.NoError(t, first.Record(ctx, "DOCS", other, "2", "B", ""))
	require.NoError(t, first.RecordVersion(ctx, "DOCS", other, 1))
	require.NoError(t, first.Save(ctx))

	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, left.RecordVersion(ctx, "DOCS", "a.md", 2))
	require.NoError(t, right.Record(ctx, "DOCS", other, "2", "B", ""))
	require.NoError(t, right.RecordVersion(ctx, "DOCS", other, 2))

	require.NoError(t, left.Save(ctx))
	require.NoError(t, right.Save(ctx))

	assert.Equal(t, map[string]int64{"a.md": 2, other: 2}, savedVersions(t, server, id),
		"each run's page version is the one recorded for its document")
	assert.Equal(t, 3, shardOf(server, id, "a.md").Version, "both writes landed")
}

// TestSaveKeepsTheLaterWriteOfAPageBothRunsPublished: two runs that overlap
// and both publish one document. Whichever saves second, the version recorded
// is that of the run that wrote the page last, which is the version the page
// is at.
func TestSaveKeepsTheLaterWriteOfAPageBothRunsPublished(t *testing.T) {
	for name, versions := range map[string][2]int64{
		"the first to save wrote last":  {3, 2},
		"the second to save wrote last": {2, 3},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			first, server := newStore(t)
			server.AddSpace("DOCS")
			id := spaceID(t, server, "DOCS")

			require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", "before"))
			require.NoError(t, first.RecordVersion(ctx, "DOCS", "a.md", 1))
			require.NoError(t, first.Save(ctx))

			runs := [2]*manifest.Store{newStoreOn(t, server), newStoreOn(t, server)}
			for i, run := range runs {
				hash := fmt.Sprintf("written-at-%d", versions[i])
				require.NoError(t, run.Record(ctx, "DOCS", "a.md", "1", "A", hash))
				require.NoError(t, run.RecordVersion(ctx, "DOCS", "a.md", versions[i]))
			}
			for _, run := range runs {
				require.NoError(t, run.Save(ctx))
			}

			assert.Equal(t, map[string]int64{"a.md": 3}, savedVersions(t, server, id))
			// A run left with nothing of its own to save writes nothing.
			assert.Equal(t, int(versions[1]), shardOf(server, id, "a.md").Version)

			entry, ok, err := newStoreOn(t, server).Recorded(ctx, "DOCS", "a.md")
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "written-at-3", entry.Hash, "the entry is the later write's as a whole")

			// The run that saved second holds what it saved.
			entry, ok, err = runs[1].Recorded(ctx, "DOCS", "a.md")
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, int64(3), entry.Version)
		})
	}
}

// TestSaveFindsARenameAmongWhatAnotherRunSaved: an entry the other run
// changed, taken in when the shard is read again, is found by its new
// fingerprint as one read at the start of the run would be.
func TestSaveFindsARenameAmongWhatAnotherRunSaved(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	other := sameShard(t, "a.md")

	require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", "before"))
	require.NoError(t, first.Save(ctx))

	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record(ctx, "DOCS", "a.md", "1", "A", "after"))
	require.NoError(t, right.Record(ctx, "DOCS", other, "2", "B", ""))

	require.NoError(t, left.Save(ctx))
	require.NoError(t, right.Save(ctx))

	path, entry, ok, err := right.ResolveRenamed(ctx, "DOCS", "after")
	require.NoError(t, err)
	require.True(t, ok, "the entry is found by the fingerprint the other run saved")
	assert.Equal(t, "a.md", path)
	assert.Equal(t, "1", entry.PageID)
}

// TestSaveKeepsBothRunsMappingsOfANewShard: two first runs that overlap both
// find no shard and both create it.
func TestSaveKeepsBothRunsMappingsOfANewShard(t *testing.T) {
	ctx := t.Context()
	left, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")
	other := sameShard(t, "a.md")

	right := newStoreOn(t, server)
	require.NoError(t, left.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, right.Record(ctx, "DOCS", other, "2", "B", ""))

	require.NoError(t, left.Save(ctx))
	require.NoError(t, right.Save(ctx))

	assert.Equal(t, map[string]string{"a.md": "1", other: "2"}, manifestPages(t, server, id))
}

// TestSaveKeepsAMappingDroppedWhileAnotherRunSaved: an entry this run dropped
// stays dropped when the other run left it alone, and what the other run
// recorded is kept.
func TestSaveKeepsAMappingDroppedWhileAnotherRunSaved(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")
	other := sameShard(t, "a.md")

	require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, first.Save(ctx))

	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record(ctx, "DOCS", other, "2", "B", ""))
	require.NoError(t, right.Forget(ctx, "DOCS", "a.md"))

	require.NoError(t, left.Save(ctx))
	require.NoError(t, right.Save(ctx))

	assert.Equal(t, map[string]string{other: "2"}, manifestPages(t, server, id))
}

// TestSaveKeepsAMappingAnotherRunRecordedAfresh: an entry this run dropped is
// kept when the other run recorded it again in the meantime.
func TestSaveKeepsAMappingAnotherRunRecordedAfresh(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, first.Save(ctx))

	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record(ctx, "DOCS", "a.md", "7", "A", ""))
	require.NoError(t, right.Forget(ctx, "DOCS", "a.md"))

	require.NoError(t, left.Save(ctx))
	require.NoError(t, right.Save(ctx))

	assert.Equal(t, map[string]string{"a.md": "7"}, manifestPages(t, server, id))
}

// TestSaveReportsAShardItCouldNotSave: a shard still refused after it was read
// again is an error naming it, one a caller can match, and the other shards
// are saved all the same.
func TestSaveReportsAShardItCouldNotSave(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")
	apart := otherShard(t, "a.md")

	require.NoError(t, first.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, first.Save(ctx))

	store := newStoreOn(t, server)
	require.NoError(t, store.Record(ctx, "DOCS", "a.md", "2", "A", ""))
	require.NoError(t, store.Record(ctx, "DOCS", apart, "3", "C", ""))

	failWrites(server, http.StatusConflict, `{"message":"version conflict"}`, shardOf(server, id, "a.md").ID)

	err := store.Save(ctx)
	require.Error(t, err, "a shard that was not saved must be reported")
	assert.ErrorIs(t, err, confluence.ErrPropertyConflict)
	assert.Contains(t, err.Error(), manifest.PropertyKey(manifest.ShardFor("a.md")))
	assert.Contains(t, err.Error(), `"DOCS"`)

	assert.Equal(t, map[string]string{"a.md": "1", apart: "3"}, manifestPages(t, server, id),
		"the shard that could be written was")

	// The entries are still held, so a save that is let through writes them.
	server.SetFail(nil)
	require.NoError(t, store.Save(ctx))
	assert.Equal(t, map[string]string{"a.md": "2", apart: "3"}, manifestPages(t, server, id))
}

// TestSaveWritesTheRestWhenOneMappingIsRefused: the folder and parent mappings
// are each one property written before the shards. A write of one that
// Confluence refuses is returned, and everything else is saved all the same.
func TestSaveWritesTheRestWhenOneMappingIsRefused(t *testing.T) {
	for _, key := range []string{manifest.FolderPropertyKey, manifest.ParentPropertyKey} {
		t.Run(key, func(t *testing.T) {
			ctx := t.Context()
			first, server := newStore(t)
			server.AddSpace("DOCS")
			id := spaceID(t, server, "DOCS")

			require.NoError(t, first.RecordFolder(ctx, "DOCS", "Guides", "folder-1"))
			require.NoError(t, first.RecordParent(ctx, "DOCS", "Handbook", "10"))
			require.NoError(t, first.Save(ctx))

			store := newStoreOn(t, server)
			require.NoError(t, store.RecordFolder(ctx, "DOCS", "Reference", "folder-2"))
			require.NoError(t, store.RecordParent(ctx, "DOCS", "Handbook / Team", "11"))
			require.NoError(t, store.Record(ctx, "DOCS", "a.md", "1", "A", ""))

			failWrites(server, http.StatusRequestEntityTooLarge, `{"message":"too large"}`,
				server.SpaceProperty(id, key).ID)

			err := store.Save(ctx)
			require.Error(t, err, "the refused write must be reported")
			var status *confluence.StatusError
			require.ErrorAs(t, err, &status)
			assert.Equal(t, http.StatusRequestEntityTooLarge, status.StatusCode)

			assert.Equal(t, map[string]string{"a.md": "1"}, manifestPages(t, server, id),
				"the page mapping is saved")
			for _, other := range []string{manifest.FolderPropertyKey, manifest.ParentPropertyKey} {
				want := 2
				if other == key {
					want = 1
				}
				assert.Equal(t, want, server.SpaceProperty(id, other).Version, other)
			}
		})
	}
}

// TestSaveWritesTheRestWhenOneShardIsRefused: one shard's failed write keeps
// neither the other shards nor the other spaces from being saved.
func TestSaveWritesTheRestWhenOneShardIsRefused(t *testing.T) {
	ctx := t.Context()
	first, server := newStore(t)
	server.AddSpace("DOCS")
	server.AddSpace("TEAM")
	docs, team := spaceID(t, server, "DOCS"), spaceID(t, server, "TEAM")

	// The refused shard is the first written: of the first space, and before
	// the shard of the other path.
	low, high := "a.md", otherShard(t, "a.md")
	if manifest.ShardFor(low) > manifest.ShardFor(high) {
		low, high = high, low
	}

	require.NoError(t, first.Record(ctx, "DOCS", low, "1", "A", ""))
	require.NoError(t, first.Save(ctx))

	store := newStoreOn(t, server)
	require.NoError(t, store.Record(ctx, "DOCS", low, "2", "A", ""))
	require.NoError(t, store.Record(ctx, "DOCS", high, "3", "B", ""))
	require.NoError(t, store.Record(ctx, "TEAM", "a.md", "4", "A", ""))

	failWrites(server, http.StatusInternalServerError, `{"message":"unavailable"}`, shardOf(server, docs, low).ID)

	require.Error(t, store.Save(ctx))

	assert.Equal(t, map[string]string{low: "1", high: "3"}, manifestPages(t, server, docs))
	assert.Equal(t, map[string]string{"a.md": "4"}, manifestPages(t, server, team))
}

// TestSaveStopsAtAWriteThatIsNoAnswerAboutItsProperty: a login Confluence does
// not accept would meet every write the same, so the first one ends the save
// instead of each property being tried in turn.
func TestSaveStopsAtAWriteThatIsNoAnswerAboutItsProperty(t *testing.T) {
	ctx := t.Context()
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.AddSpace("TEAM")

	require.NoError(t, store.RecordParent(ctx, "DOCS", "Handbook", "10"))
	require.NoError(t, store.Record(ctx, "DOCS", "a.md", "1", "A", ""))
	require.NoError(t, store.Record(ctx, "DOCS", otherShard(t, "a.md"), "2", "B", ""))
	require.NoError(t, store.Record(ctx, "TEAM", "a.md", "3", "A", ""))

	writes := 0
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/properties") {
			return 0, "", false
		}
		writes++
		return http.StatusUnauthorized, `{"message":"not logged in"}`, true
	})

	err := store.Save(ctx)
	require.ErrorIs(t, err, confluence.ErrUnauthorized)
	assert.Equal(t, 1, writes, "the writes after the first were not attempted")

	// Nothing was dropped: a save that is let through writes all of it.
	server.SetFail(nil)
	require.NoError(t, store.Save(ctx))
	assert.Len(t, manifestPages(t, server, spaceID(t, server, "DOCS")), 2)
	assert.Len(t, manifestPages(t, server, spaceID(t, server, "TEAM")), 1)
}
