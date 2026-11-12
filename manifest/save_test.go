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
