package manifest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) (*manifest.Store, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	return newStoreOn(t, server), server
}

// newStoreOn builds a store already scoped to a run, since orphan reporting is
// deliberately silent about entries whose scope is unknown.
func newStoreOn(t *testing.T, server *confluencetest.Server) *manifest.Store {
	t.Helper()
	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	store.SetRunFiles("*.md", nil)
	return store
}

// spaceID resolves a space key the way the store does, so assertions can look
// the property up by the same id.
func spaceID(t *testing.T, server *confluencetest.Server, key string) string {
	t.Helper()
	api := confluence.NewAPI(server.URL, "user", "token", false)
	id, err := api.GetSpaceID(key)
	require.NoError(t, err)
	return id
}

// manifestPages reads every shard held by owner and returns the whole mapping.
// The mapping is split across properties, so no single one of them is the
// manifest and assertions have to look at all of them.
func manifestPages(t *testing.T, server *confluencetest.Server, ownerID string) map[string]string {
	t.Helper()
	all := map[string]string{}
	for i := range manifest.ShardCount {
		property := server.SpaceProperty(ownerID, manifest.PropertyKey(i))
		if property == nil {
			continue
		}
		var doc struct {
			Pages map[string]struct {
				PageID string `json:"pageId"`
			} `json:"pages"`
		}
		require.NoError(t, json.Unmarshal(property.Value, &doc))
		for path, entry := range doc.Pages {
			all[path] = entry.PageID
		}
	}
	return all
}

// shardOf returns the stored property a path's mapping lives in, or nil.
func shardOf(server *confluencetest.Server, ownerID, path string) *confluencetest.SpaceProperty {
	return server.SpaceProperty(ownerID, manifest.PropertyKey(manifest.ShardFor(path)))
}

func TestRecordAndLookupRoundTrip(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.Record("DOCS", "docs/intro.md", "1234", "T", ""))
	require.NoError(t, store.Save())

	// A fresh store, as the next run would have.
	next := newStoreOn(t, server)
	entry, ok, err := next.Lookup("DOCS", "docs/intro.md")
	require.NoError(t, err)
	require.True(t, ok, "the mapping must survive a round trip through Confluence")
	assert.Equal(t, "1234", entry.PageID)
}

// TestLookupSurvivesTitleChange is the point of the whole package: the title
// recorded last run is not what the entry is found by, so changing it -- via the
// Title header, the leading H1 or a file rename -- still resolves to the same
// page instead of publishing a second one.
func TestLookupSurvivesTitleChange(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.Record("DOCS", "docs/intro.md", "1234", "T", ""))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	entry, ok, err := next.Lookup("DOCS", "docs/intro.md")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1234", entry.PageID,
		"the page is found by path, so its title is free to change")
}

func TestLookupMissReportsNotFound(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	_, ok, err := store.Lookup("DOCS", "docs/never-published.md")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestFirstRunHasNoProperty covers the ordinary starting state: a space that has
// never been published to has no manifest, which is not an error.
func TestFirstRunHasNoProperty(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	_, ok, err := store.Lookup("DOCS", "anything.md")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, server.SpaceProperty(spaceID(t, server, "DOCS"), manifest.PropertyKey(manifest.ShardFor("a.md"))),
		"a read must not create the property")
}

func TestOrphansAreRecordedPathsThisRunDidNotSee(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Record("DOCS", "b.md", "2", "T", ""))
	require.NoError(t, store.Record("DOCS", "c.md", "3", "T", ""))
	require.NoError(t, store.Save())

	// Next run publishes only a.md and c.md.
	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, next.Record("DOCS", "c.md", "3", "T", ""))

	assert.Equal(t, []string{"b.md"}, next.Orphans("DOCS"))
}

// TestLookupCountsAsSeen: a file that is looked up but has no entry yet is still
// part of this run, so it must not be reported as an orphan once recorded.
func TestLookupCountsAsSeen(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	_, _, err := next.Lookup("DOCS", "a.md")
	require.NoError(t, err)

	assert.Empty(t, next.Orphans("DOCS"))
}

func TestOrphansForUnloadedSpaceIsEmpty(t *testing.T) {
	store, _ := newStore(t)
	assert.Empty(t, store.Orphans("NEVER-TOUCHED"))
}

// TestSaveSkipsUnchangedSpaces pins that a repeat run over unchanged files does
// not write, so the property version does not climb for no reason.
func TestSaveSkipsUnchangedSpaces(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())
	require.Equal(t, 1, server.SpaceProperty(id, manifest.PropertyKey(manifest.ShardFor("a.md"))).Version)

	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "a.md", "1", "T", "")) // identical
	require.NoError(t, next.Save())

	assert.Equal(t, 1, server.SpaceProperty(id, manifest.PropertyKey(manifest.ShardFor("a.md"))).Version,
		"an unchanged manifest must not be rewritten")
}

func TestSaveUpdatesExistingProperty(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "b.md", "2", "T", ""))
	require.NoError(t, next.Save())

	assert.Equal(t, map[string]string{"a.md": "1", "b.md": "2"},
		manifestPages(t, server, id), "the earlier mapping must not be dropped")

	// Rewriting a path supersedes the version of the shard that holds it.
	third := newStoreOn(t, server)
	require.NoError(t, third.Record("DOCS", "a.md", "99", "T", ""))
	require.NoError(t, third.Save())

	property := shardOf(server, id, "a.md")
	require.NotNil(t, property)
	assert.Equal(t, 2, property.Version, "the rewrite supersedes the first version")
	assert.Equal(t, "99", manifestPages(t, server, id)["a.md"])
}

// TestMultipleSpaces: the `Space` header is per-document, so one run can publish
// into several spaces and each gets its own manifest.
func TestMultipleSpaces(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.AddSpace("OPS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Record("OPS", "b.md", "2", "T", ""))
	require.NoError(t, store.Save())

	assert.Equal(t, []string{"DOCS", "OPS"}, store.Spaces())

	next := newStoreOn(t, server)
	_, ok, err := next.Lookup("OPS", "b.md")
	require.NoError(t, err)
	assert.True(t, ok)

	// A path recorded in one space must not resolve in another.
	_, ok, err = next.Lookup("OPS", "a.md")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestUnreadableManifestIsIgnored: a corrupted property must not block
// publishing. Losing rename protection for one run is recoverable; refusing to
// publish is not.
func TestUnreadableManifestIsIgnored(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.SetSpaceProperty(spaceID(t, server, "DOCS"), manifest.PropertyKey(manifest.ShardFor("a.md")), []byte(`"not a manifest"`))

	_, ok, err := store.Lookup("DOCS", "a.md")
	require.NoError(t, err, "an unreadable manifest must not fail the run")
	assert.False(t, ok)
}

// TestNewerFormatIsIgnored: a manifest written by a future mark whose shape this
// one does not know is left alone rather than misread.
func TestNewerFormatIsIgnored(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.SetSpaceProperty(spaceID(t, server, "DOCS"), manifest.PropertyKey(manifest.ShardFor("a.md")),
		[]byte(`{"version":99,"pages":{"a.md":{"pageId":"1","title":"A"}}}`))

	_, ok, err := store.Lookup("DOCS", "a.md")
	require.NoError(t, err)
	assert.False(t, ok, "entries from an unknown format must not be trusted")
}

// TestConcurrentWriteIsReportedNotFatal: two runs racing on the same manifest.
// The loser keeps its published pages -- they are live either way -- and only
// loses its mapping, so a failed manifest write must not fail the run.
func TestConcurrentWriteIsReportedNotFatal(t *testing.T) {
	first, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, first.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, first.Save())

	// Both write the same path, so both land in the same shard and genuinely
	// contend. Different paths usually fall in different shards and never meet.
	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record("DOCS", "a.md", "2", "T", ""))
	require.NoError(t, right.Record("DOCS", "a.md", "3", "T", ""))

	// ...and the second to write finds its version superseded.
	require.NoError(t, left.Save())
	assert.NoError(t, right.Save(), "losing the race must not fail the run")

	property := shardOf(server, spaceID(t, server, "DOCS"), "a.md")
	require.NotNil(t, property)
	assert.Equal(t, 2, property.Version, "exactly one of the two writes landed")
}

// serverInstance makes the fake behave like Confluence Server/Data Center:
// there is no v2 API at all, so the Cloud probe fails and every v2 route 404s.
func serverInstance(t *testing.T) (*manifest.Store, *confluencetest.Server, *confluence.API) {
	t.Helper()
	server := confluencetest.New(t)
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			return http.StatusNotFound, `<html>404</html>`, true
		}
		return 0, "", false
	})
	api := confluence.NewAPI(server.URL, "user", "token", false)
	return manifest.NewStore(api), server, api
}

// docsWithHomepage gives a space the homepage the Server backend anchors to.
func docsWithHomepage(t *testing.T, server *confluencetest.Server) string {
	t.Helper()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	return home.ID
}

// TestServerBackendRoundTrip is the Data Center path: space properties do not
// exist there, so the manifest is a content property on the space homepage.
func TestServerBackendRoundTrip(t *testing.T) {
	store, server, api := serverInstance(t)
	homeID := docsWithHomepage(t, server)
	require.False(t, api.IsCloud(), "the fake must look like Server for this test")

	require.NoError(t, store.Record("DOCS", "docs/intro.md", "1234", "T", ""))
	require.NoError(t, store.Save())

	require.NotNil(t, shardOf(server, homeID, "docs/intro.md"),
		"the manifest should be stored on the homepage")

	next := newStoreOn(t, server)
	entry, ok, err := next.Lookup("DOCS", "docs/intro.md")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1234", entry.PageID)
}

// TestServerBackendUpdatesExistingProperty exercises the v1 update path, which
// addresses a property by key where v2 addresses it by id.
func TestServerBackendUpdatesExistingProperty(t *testing.T) {
	store, server, _ := serverInstance(t)
	homeID := docsWithHomepage(t, server)

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "b.md", "2", "T", ""))
	require.NoError(t, next.Save())

	assert.Equal(t, map[string]string{"a.md": "1", "b.md": "2"},
		manifestPages(t, server, homeID), "the earlier mapping must not be dropped")

	// Rewriting a path supersedes its shard, which on v1 is addressed by key.
	third := newStoreOn(t, server)
	require.NoError(t, third.Record("DOCS", "a.md", "99", "T", ""))
	require.NoError(t, third.Save())

	property := shardOf(server, homeID, "a.md")
	require.NotNil(t, property)
	assert.Equal(t, 2, property.Version, "the rewrite supersedes the first version")
	assert.Equal(t, "99", manifestPages(t, server, homeID)["a.md"])
}

// TestServerBackendFirstRunHasNoProperty: a homepage that has never been
// published to has no property, and reading it must not create one.
func TestServerBackendFirstRunHasNoProperty(t *testing.T) {
	store, server, _ := serverInstance(t)
	homeID := docsWithHomepage(t, server)

	_, ok, err := store.Lookup("DOCS", "anything.md")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, server.SpaceProperty(homeID, manifest.PropertyKey(manifest.ShardFor("a.md"))))
}

// TestServerBackendConcurrentWriteIsReportedNotFatal mirrors the Cloud case:
// the v1 endpoint versions properties too, so the loser of a race is told.
func TestServerBackendConcurrentWriteIsReportedNotFatal(t *testing.T) {
	first, server, _ := serverInstance(t)
	homeID := docsWithHomepage(t, server)

	require.NoError(t, first.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, first.Save())

	left := newStoreOn(t, server)
	right := newStoreOn(t, server)
	require.NoError(t, left.Record("DOCS", "a.md", "2", "T", ""))
	require.NoError(t, right.Record("DOCS", "a.md", "3", "T", ""))

	require.NoError(t, left.Save())
	assert.NoError(t, right.Save(), "losing the race must not fail the run")

	assert.Equal(t, 2, shardOf(server, homeID, "a.md").Version,
		"exactly one of the two writes landed")
}

// TestCloudUsesSpaceNotHomepage pins the backend split: on Cloud the manifest
// must not land on the homepage, because v1 content properties are exactly what
// a scoped API token cannot reach.
func TestCloudUsesSpaceNotHomepage(t *testing.T) {
	store, server := newStore(t)
	homeID := docsWithHomepage(t, server)

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	assert.Nil(t, shardOf(server, homeID, "a.md"),
		"Cloud must not write the manifest to the homepage")
	assert.NotNil(t, shardOf(server, spaceID(t, server, "DOCS"), "a.md"),
		"Cloud writes it to the space")
}

// TestManifestSpreadsAcrossShards is the point of splitting it up: a repository
// large enough to matter must not put its whole mapping in one property, since
// Confluence bounds how big one can be.
func TestManifestSpreadsAcrossShards(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	const files = 500
	want := map[string]string{}
	for i := range files {
		path := fmt.Sprintf("docs/section-%02d/page-%03d.md", i%20, i)
		pageID := strconv.Itoa(100000 + i)
		want[path] = pageID
		require.NoError(t, store.Record("DOCS", path, pageID, fmt.Sprintf("Page %d", i), ""))
	}
	require.NoError(t, store.Save())

	// Every mapping survives...
	assert.Equal(t, want, manifestPages(t, server, id))

	// ...spread over the shards rather than piled into one.
	used, largest := 0, 0
	for i := range manifest.ShardCount {
		property := server.SpaceProperty(id, manifest.PropertyKey(i))
		if property == nil {
			continue
		}
		used++
		largest = max(largest, len(property.Value))
	}
	assert.Equal(t, manifest.ShardCount, used, "all shards should carry a share")

	// A single blob would be the sum of them; each is a fraction of that.
	assert.Less(t, largest, 8*1024,
		"no single property should be anywhere near a plausible size limit at this scale")
}

// TestShardAssignmentIsStable pins the one property sharding must have: a path
// always resolves to the same shard. If it drifted, every existing mapping
// would be looked for in the wrong property and silently lost.
func TestShardAssignmentIsStable(t *testing.T) {
	for _, path := range []string{
		"a.md", "docs/intro.md", "docs/deeply/nested/file.md", "", "unicode/ページ.md",
	} {
		first := manifest.ShardFor(path)
		assert.Equal(t, first, manifest.ShardFor(path), "%q moved between calls", path)
		assert.GreaterOrEqual(t, first, 0)
		assert.Less(t, first, manifest.ShardCount)
	}
}

// TestSaveWritesOnlyDirtyShards: adding one file must not rewrite the whole
// mapping, or every run would bump the version of all sixteen properties and
// turn a one-page change into sixteen writes.
func TestSaveWritesOnlyDirtyShards(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	paths := []string{}
	for i := range 100 {
		paths = append(paths, fmt.Sprintf("docs/page-%03d.md", i))
	}
	for _, path := range paths {
		require.NoError(t, store.Record("DOCS", path, "1", "T", ""))
	}
	require.NoError(t, store.Save())

	before := map[int]int{}
	for i := range manifest.ShardCount {
		if property := server.SpaceProperty(id, manifest.PropertyKey(i)); property != nil {
			before[i] = property.Version
		}
	}

	// One new file, in exactly one shard.
	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "docs/brand-new.md", "999", "T", ""))
	require.NoError(t, next.Save())

	touched := 0
	for i := range manifest.ShardCount {
		property := server.SpaceProperty(id, manifest.PropertyKey(i))
		if property != nil && property.Version != before[i] {
			touched++
		}
	}
	assert.Equal(t, 1, touched, "only the shard holding the new path should be rewritten")
}

// TestUnreadableShardOnlyLosesItsOwnPaths: a corrupted property must not take
// the rest of the mapping down with it, which is the other reason to split.
func TestUnreadableShardOnlyLosesItsOwnPaths(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Record("DOCS", "b.md", "2", "T", ""))
	require.NoError(t, store.Save())
	require.NotEqual(t, manifest.ShardFor("a.md"), manifest.ShardFor("b.md"),
		"this test needs the two paths in different shards")

	server.SetSpaceProperty(id, manifest.PropertyKey(manifest.ShardFor("a.md")), []byte(`"corrupt"`))

	next := newStoreOn(t, server)
	_, ok, err := next.Lookup("DOCS", "a.md")
	require.NoError(t, err)
	assert.False(t, ok, "the corrupted shard's paths are lost")

	entry, ok, err := next.Lookup("DOCS", "b.md")
	require.NoError(t, err)
	require.True(t, ok, "an intact shard must still resolve")
	assert.Equal(t, "2", entry.PageID)
}

// TestReadTakesOneRequestPerSpace: sixteen properties must not mean sixteen
// round trips. They are listed in one call.
func TestReadTakesOneRequestPerSpace(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	server.ResetRequests()
	_, _, err := next.Lookup("DOCS", "a.md")
	require.NoError(t, err)

	assert.Equal(t, 1, server.CountRequests("GET", "/properties"),
		"all shards should be read in a single listing")
}

// TestFolderMappingRoundTrip covers the folder half. Folders are found by title
// and created when the title is not found, so one renamed in Confluence stops
// matching the header that declares it and mark builds a second beside it.
func TestFolderMappingRoundTrip(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.RecordFolder("DOCS", "anchor\x00Guides", "folder-1"))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	id, ok, err := next.LookupFolder("DOCS", "anchor\x00Guides")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "folder-1", id)
}

// TestFolderMappingIsKeptOutOfThePageShards: a folder key appearing among the
// page keys would be reported as a source file that had gone missing.
func TestFolderMappingIsKeptOutOfThePageShards(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "A", ""))
	require.NoError(t, store.RecordFolder("DOCS", "anchor\x00Guides", "folder-1"))
	require.NoError(t, store.Save())

	assert.Equal(t, map[string]string{"a.md": "1"}, manifestPages(t, server, id),
		"folders must not appear among the tracked source paths")
	assert.NotNil(t, server.SpaceProperty(id, manifest.FolderPropertyKey),
		"they live in their own property")

	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "a.md", "1", "A", ""))
	assert.Empty(t, next.Orphans("DOCS"),
		"a folder key must never be reported as a missing file")
}

// TestParentMappingRoundTrip covers the parent half. Parents are found by title
// too, so one renamed in Confluence stops matching the header that declares it
// and mark creates an empty page under the old name.
func TestParentMappingRoundTrip(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	require.NoError(t, store.RecordParent("DOCS", "Docs\x00Team", "page-1"))
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	id, ok, err := next.LookupParent("DOCS", "Docs\x00Team")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "page-1", id)
}

// TestParentMappingIsKeptOutOfThePageShards: a parent key among the page keys
// would be reported as a source file that had gone missing, and most parents
// have no source file at all.
func TestParentMappingIsKeptOutOfThePageShards(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "A", ""))
	require.NoError(t, store.RecordParent("DOCS", "Docs", "page-1"))
	require.NoError(t, store.Save())

	assert.Equal(t, map[string]string{"a.md": "1"}, manifestPages(t, server, id),
		"parents must not appear among the tracked source paths")
	assert.NotNil(t, server.SpaceProperty(id, manifest.ParentPropertyKey),
		"they live in their own property")

	next := newStoreOn(t, server)
	require.NoError(t, next.Record("DOCS", "a.md", "1", "A", ""))
	assert.Empty(t, next.Orphans("DOCS"),
		"a parent key must never be reported as a missing file")
}

// TestParentMappingSkippedWhenUnchanged: parents move even less than folders do,
// so a run that resolves the same chain must not rewrite the property.
func TestParentMappingSkippedWhenUnchanged(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.RecordParent("DOCS", "Docs", "page-1"))
	require.NoError(t, store.Save())
	require.Equal(t, 1, server.SpaceProperty(id, manifest.ParentPropertyKey).Version)

	next := newStoreOn(t, server)
	require.NoError(t, next.RecordParent("DOCS", "Docs", "page-1"))
	require.NoError(t, next.Save())

	assert.Equal(t, 1, server.SpaceProperty(id, manifest.ParentPropertyKey).Version,
		"an unchanged parent mapping must not be rewritten")
}

// TestFolderMappingSkippedWhenUnchanged: folders rarely move, so a run that
// resolves the same ones must not rewrite the property.
func TestFolderMappingSkippedWhenUnchanged(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	require.NoError(t, store.RecordFolder("DOCS", "anchor\x00Guides", "folder-1"))
	require.NoError(t, store.Save())
	require.Equal(t, 1, server.SpaceProperty(id, manifest.FolderPropertyKey).Version)

	next := newStoreOn(t, server)
	require.NoError(t, next.RecordFolder("DOCS", "anchor\x00Guides", "folder-1"))
	require.NoError(t, next.Save())

	assert.Equal(t, 1, server.SpaceProperty(id, manifest.FolderPropertyKey).Version,
		"an unchanged folder mapping must not be rewritten")
}

// TestResolveStaleTitleReportsReadFailures: a manifest that cannot be read is
// not the same as a title that was never published. Folding the two together
// would create a duplicate page because the network hiccuped.
func TestResolveStaleTitleReportsReadFailures(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/properties") {
			return http.StatusInternalServerError, `{"message":"nope"}`, true
		}
		return 0, "", false
	})

	store := newStoreOn(t, server)
	_, ok, err := store.ResolveStaleTitle("DOCS", "Some Title")
	require.Error(t, err, "an unreadable manifest must be reported, not silently treated as empty")
	assert.False(t, ok)
}

// TestKeyNormalisesToTheWorkingDirectory pins what makes a key mean the same
// thing in two places. A glob of "$PWD/docs/*.md" and one of "docs/*.md" name
// the same files; keyed as given they would keep two disjoint mappings of one
// repository, and an absolute key would additionally embed a checkout
// directory that does not survive being moved.
func TestKeyNormalisesToTheWorkingDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, "docs/a.md", manifest.Key("docs/a.md"))
	assert.Equal(t, "docs/a.md", manifest.Key("./docs/a.md"))
	assert.Equal(t, "docs/a.md", manifest.Key("docs/../docs/a.md"))
	assert.Equal(t, "docs/a.md", manifest.Key(filepath.Join(cwd, "docs/a.md")),
		"an absolute path inside the working directory is stored relative to it")

	// Outside it there is no better anchor, and mark has no notion of a project
	// root to invent one from.
	outside := filepath.Join(filepath.Dir(cwd), "elsewhere", "a.md")
	assert.Equal(t, filepath.ToSlash(outside), manifest.Key(outside))
}

// TestAbsoluteAndRelativeRunsShareOneMapping is the behaviour that matters: the
// same file published twice, once through each form of glob, is one entry.
func TestAbsoluteAndRelativeRunsShareOneMapping(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	cwd, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, store.Record("DOCS", "docs/a.md", "1", "A", "h"))
	require.NoError(t, store.Record("DOCS", filepath.Join(cwd, "docs/a.md"), "1", "A", "h"))
	require.NoError(t, store.Save())

	assert.Equal(t, map[string]string{"docs/a.md": "1"}, manifestPages(t, server, id),
		"the two forms of the same path must be one entry, not two")
}

// TestManifestWrittenBeforeHashesAndGlobsStillResolves: entries written by an
// earlier mark carry no fingerprint and no pattern. They must still place their
// pages -- losing the mapping on upgrade would republish every renamed document
// as a duplicate, which is the failure the mapping exists to prevent.
func TestManifestWrittenBeforeHashesAndGlobsStillResolves(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")

	// The shape as an earlier version wrote it: page id and title only.
	server.SetSpaceProperty(spaceID(t, server, "DOCS"),
		manifest.PropertyKey(manifest.ShardFor("a.md")),
		[]byte(`{"version":1,"pages":{"a.md":{"pageId":"1234","title":"A"}}}`))

	entry, ok, err := store.Lookup("DOCS", "a.md")
	require.NoError(t, err)
	require.True(t, ok, "an entry from before hashes and globs must still resolve")
	assert.Equal(t, "1234", entry.PageID)
	assert.Equal(t, "A", entry.Title)
	assert.Empty(t, entry.Hash)
	assert.Empty(t, entry.Glob)
}

// TestHashlessEntryIsNeverMatchedAsARename: with no fingerprint there is
// nothing to match on, and matching everything with an empty hash would rebind
// unrelated documents onto each other.
func TestHashlessEntryIsNeverMatchedAsARename(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.SetSpaceProperty(spaceID(t, server, "DOCS"),
		manifest.PropertyKey(manifest.ShardFor("a.md")),
		[]byte(`{"version":1,"pages":{"a.md":{"pageId":"1234","title":"A"}}}`))

	_, _, ok, err := store.ResolveRenamed("DOCS", "")
	require.NoError(t, err)
	assert.False(t, ok, "an empty fingerprint must match nothing")
}

// TestGloblessEntryIsNeverReportedMissing: without a recorded pattern there is
// no way to know whether a run was looking where the file used to be, so its
// absence proves nothing.
func TestGloblessEntryIsNeverReportedMissing(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	server.SetSpaceProperty(spaceID(t, server, "DOCS"),
		manifest.PropertyKey(manifest.ShardFor("a.md")),
		[]byte(`{"version":1,"pages":{"a.md":{"pageId":"1234","title":"A"}}}`))

	_, _, err := store.Lookup("DOCS", "unrelated.md")
	require.NoError(t, err)
	assert.Empty(t, store.Orphans("DOCS"),
		"an entry of unknown scope must not be reported as a deleted file")
}

// TestKeyIsSlashSeparated: the separator is a property of the machine, not of
// the repository. Leaving it in gives a team publishing from Windows and from
// Linux CI two entries for every file.
//
// What this can check is limited by where it runs. filepath is compiled for one
// platform, so on a slash platform Join already produces slashes and the
// assertion is nearly free; it earns its keep on Windows, and here it stands as
// a statement of the invariant rather than a demonstration of it.
func TestKeyIsSlashSeparated(t *testing.T) {
	assert.Equal(t, "docs/guides/a.md", manifest.Key("docs/guides/a.md"))
	assert.NotContains(t, manifest.Key(filepath.Join("docs", "guides", "a.md")), `\`,
		"a key must never carry a platform separator")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.NotContains(t, manifest.Key(filepath.Join(cwd, "docs", "a.md")), `\`)
}

// TestKeysWrittenBeforeNormalisationAreMigrated: an upgrade must not strand the
// mapping. A stranded entry has no pattern to report it and no fingerprint to
// match it, so it becomes dead weight that arrives once and never leaves.
func TestKeysWrittenBeforeNormalisationAreMigrated(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	oldKey := filepath.Join(cwd, "docs", "a.md")

	// The shape an earlier mark wrote: an absolute, unnormalised key.
	old, err := json.Marshal(map[string]any{
		"version": 1,
		"pages":   map[string]any{oldKey: map[string]any{"pageId": "1234", "title": "A"}},
	})
	require.NoError(t, err)
	server.SetSpaceProperty(id, manifest.PropertyKey(manifest.ShardFor(oldKey)), old)

	// The same file, asked for the way it is written now.
	entry, ok, err := store.Lookup("DOCS", "docs/a.md")
	require.NoError(t, err)
	require.True(t, ok, "an entry written before normalisation must still be found")
	assert.Equal(t, "1234", entry.PageID)

	// And the repair is persisted, so it happens once rather than every run.
	require.NoError(t, store.Record("DOCS", "docs/a.md", "1234", "A", "h"))
	require.NoError(t, store.Save())
	assert.Equal(t, map[string]string{"docs/a.md": "1234"}, manifestPages(t, server, id),
		"the old key should be gone, not sitting beside the new one")
}

// TestUnseenPropertyDoesNotAbandonTheRestOfTheSave: a 409 on a create raises
// ErrPropertyUnseen, which was deliberately not an ErrPropertyConflict -- so no
// caller recognised it, Save returned on the first one, and the run failed.
//
// Save writes the folder mapping, then the parents, then seventeen shards, so a
// collision on the first of those abandoned everything after it. Every one of
// those describes pages the failed write says nothing about.
func TestUnseenPropertyDoesNotAbandonTheRestOfTheSave(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	id := spaceID(t, server, "DOCS")

	// Recording loads the space, so the listing this run works from happens
	// here -- and finds nothing.
	require.NoError(t, store.RecordFolder("DOCS", "anchor\x00Guides", "folder-1"))
	require.NoError(t, store.Record("DOCS", "a.md", "1", "A", ""))

	// Only then does the folder mapping appear, so the create below collides
	// with a key this run's listing never showed.
	server.SetSpaceProperty(id, manifest.FolderPropertyKey, []byte(`{"version":1,"folders":{}}`))

	require.NoError(t, store.Save(),
		"one unseen property must not fail the whole save")

	assert.Equal(t, map[string]string{"a.md": "1"}, manifestPages(t, server, id),
		"the page shards are written even though the folder mapping collided")
}

// TestManifestPageOnCloud is issue #1020: creating a space property takes
// space administration, and a scoped API token is refused it outright, so the
// mapping can be kept as content properties of a page instead -- through v2,
// which such a token can reach.
func TestManifestPageOnCloud(t *testing.T) {
	store, server := newStore(t)
	homeID := docsWithHomepage(t, server)
	handbook := server.AddPage("DOCS", "Team Handbook", "page", homeID)
	store.SetManifestPage("Team Handbook")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	assert.NotNil(t, shardOf(server, handbook.ID, "a.md"), "the manifest lives on the page named")
	assert.Nil(t, shardOf(server, spaceID(t, server, "DOCS"), "a.md"), "and not on the space")
	assert.Nil(t, shardOf(server, homeID, "a.md"), "nor on the homepage")

	assert.Equal(t, 0, server.CountRequests("GET", "/rest/api/content/"+handbook.ID+"/property"),
		"read through v2, which a scoped token can reach")
	assert.Equal(t, 0, server.CountRequests("POST", "/rest/api/content/"+handbook.ID+"/property"),
		"written through v2 too")

	next := newStoreOn(t, server)
	next.SetManifestPage("Team Handbook")
	entry, ok, err := next.Lookup("DOCS", "a.md")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", entry.PageID)
}

// TestManifestPageByID: an id is used as it is.
func TestManifestPageByID(t *testing.T) {
	store, server := newStore(t)
	homeID := docsWithHomepage(t, server)
	handbook := server.AddPage("DOCS", "Team Handbook", "page", homeID)
	store.SetManifestPage(handbook.ID)

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	assert.NotNil(t, shardOf(server, handbook.ID, "a.md"))
}

// TestManifestPageByIDRefusesASecondSpace: an id names the same page for every
// space, and one page holds one space's manifest. Two spaces sharing it used to
// write the same shard twice -- the second write against a version the first
// had superseded -- and the second space's mapping was lost with Save
// returning nil. The second space is refused instead, and the first space's
// mapping is kept.
func TestManifestPageByIDRefusesASecondSpace(t *testing.T) {
	store, server := newStore(t)
	homeID := docsWithHomepage(t, server)
	server.AddSpace("OPS")
	handbook := server.AddPage("DOCS", "Team Handbook", "page", homeID)
	store.SetManifestPage(handbook.ID)

	// A path in the same shard as a.md, which is where the writes collided.
	other := ""
	for i := 0; other == "" || manifest.ShardFor(other) != manifest.ShardFor("a.md"); i++ {
		other = fmt.Sprintf("b%d.md", i)
	}

	require.NoError(t, store.Record("DOCS", "a.md", "1", "A", ""))
	err := store.Record("OPS", other, "2", "B", "")
	require.Error(t, err, "a second space on the same page is refused")
	assert.Contains(t, err.Error(), "--manifest-page")
	assert.Contains(t, err.Error(), `"DOCS"`)
	assert.Contains(t, err.Error(), `"OPS"`)
	require.NoError(t, store.Save())

	next := newStoreOn(t, server)
	next.SetManifestPage(handbook.ID)
	entry, ok, err := next.Lookup("DOCS", "a.md")
	require.NoError(t, err)
	require.True(t, ok, "the first space's mapping is kept")
	assert.Equal(t, "1", entry.PageID)
}

// TestManifestPageByTitleKeepsSpacesApart: a title is looked up in each space,
// so two spaces in one run find a page each and keep a manifest each.
func TestManifestPageByTitleKeepsSpacesApart(t *testing.T) {
	store, server := newStore(t)
	homeID := docsWithHomepage(t, server)
	server.AddSpace("OPS")
	docs := server.AddPage("DOCS", "Manifest", "page", homeID)
	ops := server.AddPage("OPS", "Manifest", "page", "")
	store.SetManifestPage("Manifest")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "A", ""))
	require.NoError(t, store.Record("OPS", "b.md", "2", "B", ""))
	require.NoError(t, store.Save())

	assert.NotNil(t, shardOf(server, docs.ID, "a.md"))
	assert.NotNil(t, shardOf(server, ops.ID, "b.md"))
}

// TestManifestPageMustExist: a page that cannot be found is refused rather
// than the mapping being quietly kept somewhere else.
func TestManifestPageMustExist(t *testing.T) {
	store, server := newStore(t)
	docsWithHomepage(t, server)
	store.SetManifestPage("Nowhere")

	err := store.Record("DOCS", "a.md", "1", "T", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--manifest-page")
	assert.Contains(t, err.Error(), "Nowhere")
}

// TestManifestPageOnServer: the same choice on Server and Data Center moves the
// mapping off the homepage, through the v1 endpoint that is all they have.
func TestManifestPageOnServer(t *testing.T) {
	store, server, api := serverInstance(t)
	homeID := docsWithHomepage(t, server)
	handbook := server.AddPage("DOCS", "Team Handbook", "page", homeID)
	require.False(t, api.IsCloud())
	store.SetManifestPage("Team Handbook")

	require.NoError(t, store.Record("DOCS", "a.md", "1", "T", ""))
	require.NoError(t, store.Save())

	assert.NotNil(t, shardOf(server, handbook.ID, "a.md"), "the manifest lives on the page named")
	assert.Nil(t, shardOf(server, homeID, "a.md"), "and not on the homepage")
	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/content/"+handbook.ID+"/property"),
		"read through v1, which is all Server has")
}

// TestPrefixKeepsTwoProjectsApart is issue #1016: two projects publishing into
// one space each keep a manifest of their own, and neither reads the other's
// entries nor reports the other's files as gone.
func TestPrefixKeepsTwoProjectsApart(t *testing.T) {
	server := confluencetest.New(t)
	docsWithHomepage(t, server)
	id := spaceID(t, server, "DOCS")

	api := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	require.NoError(t, api.SetPropertyKeyPrefix("mark.manifest.api"))
	api.SetRunFiles("api/*.md", []string{"api/a.md"})
	require.NoError(t, api.Record("DOCS", "api/a.md", "1", "A", ""))
	require.NoError(t, api.RecordFolder("DOCS", "API Folder", "f1"))
	require.NoError(t, api.Save())

	sdk := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	require.NoError(t, sdk.SetPropertyKeyPrefix("mark.manifest.sdk"))
	sdk.SetRunFiles("sdk/*.md", []string{"sdk/b.md"})
	require.NoError(t, sdk.Record("DOCS", "sdk/b.md", "2", "B", ""))
	require.NoError(t, sdk.Save())

	// Each under its own keys.
	assert.NotNil(t, server.SpaceProperty(id, manifest.PropertyKeyFor("mark.manifest.api", manifest.ShardFor("api/a.md"))))
	assert.NotNil(t, server.SpaceProperty(id, manifest.FolderPropertyKeyFor("mark.manifest.api")))
	assert.NotNil(t, server.SpaceProperty(id, manifest.PropertyKeyFor("mark.manifest.sdk", manifest.ShardFor("sdk/b.md"))))
	assert.Nil(t, server.SpaceProperty(id, manifest.PropertyKey(manifest.ShardFor("api/a.md"))),
		"nothing under the default prefix")

	// The SDK's next run knows nothing of the API's files.
	next := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	require.NoError(t, next.SetPropertyKeyPrefix("mark.manifest.sdk"))
	next.SetRunFiles("sdk/*.md", []string{"sdk/b.md"})
	_, ok, err := next.Lookup("DOCS", "api/a.md")
	require.NoError(t, err)
	assert.False(t, ok, "the API project's entry is not the SDK project's to see")
	_, ok, err = next.Lookup("DOCS", "sdk/b.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, next.Orphans("DOCS"), "and its files are not the SDK project's to report")
}

// TestPrefixIsValidated: a prefix that could not name a property is refused
// before anything is read, and an empty one means the default.
func TestPrefixIsValidated(t *testing.T) {
	store, _ := newStore(t)

	for _, bad := range []string{"has space", "trailing.", ".leading", "a..b", "semi;colon"} {
		assert.Error(t, store.SetPropertyKeyPrefix(bad), bad)
	}
	for _, good := range []string{"mark.manifest", "team-docs", "api_v2.manifest", "x"} {
		assert.NoError(t, store.SetPropertyKeyPrefix(good), good)
	}

	require.NoError(t, store.SetPropertyKeyPrefix(""), "empty means the default")
}

// captureLog sends the package logger to a buffer for the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() { log.Logger = restore })
	return &logged
}

// runStore is the store of a later run of "*.md" publishing exactly files.
func runStore(t *testing.T, server *confluencetest.Server, files ...string) *manifest.Store {
	t.Helper()
	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	store.SetRunFiles("*.md", files)
	return store
}

// TestRecordFollowsAMovedDocument: a path recorded under this run's pattern
// and absent from it, whose page another path now publishes to, is that
// document moved. Its entry goes, the move is said, and the version
// --no-overwrite compares against comes along to the new path rather than
// starting again from nothing.
func TestRecordFollowsAMovedDocument(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	require.NoError(t, store.Record("DOCS", "doc.md", "1", "Doc", "h"))
	require.NoError(t, store.RecordVersion("DOCS", "doc.md", 7))
	require.NoError(t, store.Save())

	logged := captureLog(t)
	next := runStore(t, server, "sub/doc.md")
	require.NoError(t, next.Record("DOCS", "sub/doc.md", "1", "Doc", "h"))

	assert.Contains(t, logged.String(), "doc.md moved to sub/doc.md; page 1 follows it")
	assert.NotContains(t, logged.String(), "both publish")

	entry, ok, err := next.Lookup("DOCS", "sub/doc.md")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(7), entry.Version, "the moved document keeps its version baseline")
	assert.Empty(t, next.Orphans("DOCS"))

	require.NoError(t, next.Save())
	assert.Equal(t, map[string]string{"sub/doc.md": "1"}, manifestPages(t, server, spaceID(t, server, "DOCS")),
		"the path it moved from is dropped")

	// The page is the new path's now: a third document claiming it is in
	// conflict with that one, not with the path that is gone.
	logged.Reset()
	require.NoError(t, next.Record("DOCS", "other.md", "1", "Other", "o"))
	assert.Contains(t, logged.String(), "sub/doc.md and other.md both publish to page 1")
}

// TestLookupMovedAnswersForTheMovingRun: before Record carries a moved
// document's entry across, its new path has none, and what the manifest knows
// about the page is in the entry of the path it moved from. Nothing is
// answered for a page whose owner the run still publishes.
func TestLookupMovedAnswersForTheMovingRun(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	require.NoError(t, store.Record("DOCS", "doc.md", "1", "Doc", "h"))
	require.NoError(t, store.RecordVersion("DOCS", "doc.md", 7))
	require.NoError(t, store.Save())

	next := runStore(t, server, "sub/doc.md")
	entry, ok, err := next.LookupMoved("DOCS", "sub/doc.md", "1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(7), entry.Version)

	_, ok, err = next.LookupMoved("DOCS", "sub/doc.md", "2")
	require.NoError(t, err)
	assert.False(t, ok, "a page nobody recorded has no previous owner")

	stays := runStore(t, server, "doc.md", "sub/doc.md")
	_, ok, err = stays.LookupMoved("DOCS", "sub/doc.md", "1")
	require.NoError(t, err)
	assert.False(t, ok, "an owner this run publishes has not moved")
}

// TestRecordDoesNotFollowAnotherPatternsDocument: a path recorded by another
// --files pattern is not this run's to call gone, so a second document
// publishing to its page is a conflict, warned about, and the entry stays.
func TestRecordDoesNotFollowAnotherPatternsDocument(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	other := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	other.SetRunFiles("other/*.md", []string{"other/doc.md"})
	require.NoError(t, other.Record("DOCS", "other/doc.md", "1", "Doc", "h"))
	require.NoError(t, other.RecordVersion("DOCS", "other/doc.md", 7))
	require.NoError(t, other.Save())

	logged := captureLog(t)
	next := runStore(t, server, "doc.md")
	require.NoError(t, next.Record("DOCS", "doc.md", "1", "Doc", "h"))

	assert.Contains(t, logged.String(), "other/doc.md and doc.md both publish to page 1")
	assert.NotContains(t, logged.String(), "moved")

	entry, ok, err := next.Lookup("DOCS", "doc.md")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Zero(t, entry.Version, "a version belongs to the path that wrote it")

	require.NoError(t, next.Save())
	assert.Equal(t, map[string]string{"other/doc.md": "1", "doc.md": "1"},
		manifestPages(t, server, spaceID(t, server, "DOCS")))
}

// TestRecordUpdatesTheOwnerOfAnUnchangedEntry: a path whose entry is already
// exactly right returns early, and the page it took over from a moved owner
// must still be its own afterwards rather than the deleted path's.
//
// Two entries for one page are loaded in shard order, the later shard's path
// becoming the owner, so the moved path is chosen to sit in a later shard than
// the one that stays.
func TestRecordUpdatesTheOwnerOfAnUnchangedEntry(t *testing.T) {
	const current = "sub/doc.md"
	gone := ""
	for i := range 100 {
		candidate := fmt.Sprintf("doc%d.md", i)
		if manifest.ShardFor(candidate) > manifest.ShardFor(current) {
			gone = candidate
			break
		}
	}
	require.NotEmpty(t, gone, "no path found in a later shard than %s", current)

	// Both published by one run, so neither is taken for the other moved.
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	store := runStore(t, server, current, gone)
	require.NoError(t, store.Record("DOCS", current, "1", "Doc", "h"))
	require.NoError(t, store.Record("DOCS", gone, "1", "Doc", "h"))
	require.NoError(t, store.Save())

	logged := captureLog(t)
	next := runStore(t, server, current, "other.md")
	require.NoError(t, next.Record("DOCS", current, "1", "Doc", "h"))
	assert.Contains(t, logged.String(), gone+" moved to "+current)

	logged.Reset()
	require.NoError(t, next.Record("DOCS", "other.md", "1", "Other", "o"))
	assert.Contains(t, logged.String(), current+" and other.md both publish to page 1",
		"the conflict is with the path that publishes the page, not the one that moved away")
}

// TestOrphansForgetStaleEntriesForAClaimedPage: a page this run published is
// the publishing document's, so no other entry for it is offered as an
// orphan -- and one whose path is gone from the run is forgotten, rather than
// lingering to be matched by its fingerprint or title on every run after.
func TestOrphansForgetStaleEntriesForAClaimedPage(t *testing.T) {
	// Both published by one run, so neither is taken for the other moved.
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	store := runStore(t, server, "a.md", "b.md", "kept.md")
	require.NoError(t, store.Record("DOCS", "a.md", "1", "Doc", "h"))
	require.NoError(t, store.Record("DOCS", "b.md", "1", "Doc", "h"))
	require.NoError(t, store.Record("DOCS", "kept.md", "2", "Kept", "k"))
	require.NoError(t, store.Save())

	// a.md and b.md are both gone; c.md publishes their page. Only one of
	// them is the page's owner and so followed as a move; the other is stale.
	next := runStore(t, server, "c.md", "kept.md")
	require.NoError(t, next.Record("DOCS", "c.md", "1", "Doc", "h"))
	require.NoError(t, next.Record("DOCS", "kept.md", "2", "Kept", "k"))

	assert.Empty(t, next.Orphans("DOCS"), "a page this run published is never an orphan")

	require.NoError(t, next.Save())
	assert.Equal(t, map[string]string{"c.md": "1", "kept.md": "2"},
		manifestPages(t, server, spaceID(t, server, "DOCS")),
		"the stale entry is forgotten, not kept forever")
}

// TestOrphansKeepAnUnseenPathThisRunPublishes: a path the run publishes that
// was not seen failed before it was recorded. Its page being claimed by
// another document says nothing about the file being gone, so it is neither
// offered as an orphan nor forgotten.
func TestOrphansKeepAnUnseenPathThisRunPublishes(t *testing.T) {
	store, server := newStore(t)
	server.AddSpace("DOCS")
	require.NoError(t, store.Record("DOCS", "a.md", "1", "Doc", "h"))
	require.NoError(t, store.Save())

	next := runStore(t, server, "a.md", "c.md")
	require.NoError(t, next.Record("DOCS", "c.md", "1", "Doc", "h"))

	assert.Empty(t, next.Orphans("DOCS"))

	require.NoError(t, next.Save())
	assert.Equal(t, map[string]string{"a.md": "1", "c.md": "1"},
		manifestPages(t, server, spaceID(t, server, "DOCS")))
}

// TestRecordDoesNotFollowAPathThisRunRecorded: a directory's page is recorded
// under a key that is never among the run's files, so only having been seen
// says that it is not gone. A document landing on a directory page recorded
// earlier in the same run is a conflict to report, not the directory moved,
// and the directory keeps its entry.
func TestRecordDoesNotFollowAPathThisRunRecorded(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	store := runStore(t, server, "doc.md")

	logged := captureLog(t)
	require.NoError(t, store.Record("DOCS", "dir", "1", "Dir", "d"))
	require.NoError(t, store.Record("DOCS", "doc.md", "1", "Doc", "h"))

	assert.Contains(t, logged.String(), "dir and doc.md both publish to page 1")
	assert.NotContains(t, logged.String(), "moved")

	require.NoError(t, store.Save())
	assert.Equal(t, map[string]string{"dir": "1", "doc.md": "1"},
		manifestPages(t, server, spaceID(t, server, "DOCS")),
		"the directory's entry is kept")
}

// TestRecordDoesNotFollowAnEntryWithNoPattern: an entry recorded before
// patterns were has no scope to be absent from, so a run with no pattern of
// its own must not read the two empty patterns as a match and take the entry
// for a moved document.
func TestRecordDoesNotFollowAnEntryWithNoPattern(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	legacy := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	require.NoError(t, legacy.Record("DOCS", "doc.md", "1", "Doc", "h"))
	require.NoError(t, legacy.Save())

	logged := captureLog(t)
	next := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	_, ok, err := next.LookupMoved("DOCS", "sub/doc.md", "1")
	require.NoError(t, err)
	assert.False(t, ok, "an entry with no pattern has not moved")

	require.NoError(t, next.Record("DOCS", "sub/doc.md", "1", "Doc", "h"))
	assert.Contains(t, logged.String(), "doc.md and sub/doc.md both publish to page 1")
	assert.NotContains(t, logged.String(), "moved")

	require.NoError(t, next.Save())
	assert.Equal(t, map[string]string{"doc.md": "1", "sub/doc.md": "1"},
		manifestPages(t, server, spaceID(t, server, "DOCS")),
		"the legacy entry is kept")
}
