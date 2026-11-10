package manifest_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeFor is a store for a run of files under glob, when glob is set, and of
// documents under scope.
func storeFor(server *confluencetest.Server, glob, scope string, files, documents []string) *manifest.Store {
	store := manifest.NewStore(confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}))
	if glob != "" {
		store.SetRunFiles(glob, files)
	}
	store.SetRunDocuments(scope, documents)

	return store
}

// TestDocumentScopeOrphans: a document is an orphan only to a later run of
// its own scope, and a document with no scope is never one.
func TestDocumentScopeOrphans(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	server.AddSpace("DOCS")

	first := storeFor(server, "*.md", "gen", []string{"file.md"}, []string{"scoped.md"})
	require.NoError(t, first.Record(ctx, "DOCS", "file.md", "1", "File", ""))
	require.NoError(t, first.Record(ctx, "DOCS", "scoped.md", "2", "Scoped", ""))
	require.NoError(t, first.Save(ctx))

	unscoped := storeFor(server, "", "", nil, []string{"loose.md"})
	require.NoError(t, unscoped.Record(ctx, "DOCS", "loose.md", "3", "Loose", ""))
	require.NoError(t, unscoped.Save(ctx))

	// The same files, no documents: the scoped one is not the pattern's.
	files := storeFor(server, "*.md", "", []string{"file.md"}, nil)
	require.NoError(t, files.Record(ctx, "DOCS", "file.md", "1", "File", ""))
	assert.Empty(t, files.Orphans("DOCS"))

	// The same scope, without scoped.md: it is that scope's orphan.
	scope := storeFor(server, "", "gen", nil, []string{"other.md"})
	require.NoError(t, scope.Record(ctx, "DOCS", "other.md", "4", "Other", ""))
	assert.Equal(t, []string{"scoped.md"}, scope.Orphans("DOCS"))

	// No scope: nothing is anybody's orphan, loose.md included.
	none := storeFor(server, "", "", nil, []string{"other.md"})
	require.NoError(t, none.Record(ctx, "DOCS", "other.md", "4", "Other", ""))
	assert.Empty(t, none.Orphans("DOCS"))
}

// TestResolveRenamedForStaysInScope: a document is only taken for a rename
// of one recorded under its own scope, and never without one.
func TestResolveRenamedForStaysInScope(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	server.AddSpace("DOCS")

	first := storeFor(server, "*.md", "gen", []string{"file.md"}, []string{"old.md"})
	require.NoError(t, first.Record(ctx, "DOCS", "file.md", "1", "File", "same"))
	require.NoError(t, first.Record(ctx, "DOCS", "old.md", "2", "Old", "same"))
	require.NoError(t, first.Save(ctx))

	next := storeFor(server, "*.md", "gen", nil, []string{"new.md"})
	path, entry, ok, err := next.ResolveRenamedFor(ctx, "DOCS", "new.md", "same")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "old.md", path)
	assert.Equal(t, "2", entry.PageID)

	unscoped := storeFor(server, "*.md", "", nil, []string{"new.md"})
	_, _, ok, err = unscoped.ResolveRenamedFor(ctx, "DOCS", "new.md", "same")
	require.NoError(t, err)
	assert.False(t, ok)
}
