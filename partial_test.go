package mark

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// partialFixture publishes a.md, b.md and c.md under one pattern with
// --track-pages, as the run that owns them does.
func partialFixture(t *testing.T) (Config, string, func() *manifest.Store) {
	t.Helper()

	server, _ := docsSpace(t)
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeFile(t, dir, name+".md", markdownWithTitle("Page "+name))
	}

	full := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), full))

	// A store reads the manifest once, so each look gets its own.
	store := func() *manifest.Store {
		return manifest.NewStore(confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}))
	}

	return full, dir, store
}

func tracked(t *testing.T, store func() *manifest.Store, file string) (manifest.Entry, bool) {
	t.Helper()

	entry, ok, err := store().Lookup(context.Background(), "DOCS", file)
	require.NoError(t, err)

	return entry, ok
}

// TestPartialRunKeepsTheFilesItLeftOut: a job publishing only the files a
// change touched gives the owning run's pattern on a checkout holding just
// those files. Without --partial the absent files are reported once and
// forgotten, losing their --no-overwrite baseline and rename tracking.
func TestPartialRunKeepsTheFilesItLeftOut(t *testing.T) {
	full, dir, store := partialFixture(t)
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	partial := full
	partial.Tracking.Partial = true
	require.NoError(t, Run(context.Background(), partial))

	for _, name := range []string{"a", "b", "c"} {
		_, ok := tracked(t, store, filepath.Join(dir, name+".md"))
		assert.True(t, ok, "%s.md is still tracked", name)
	}
}

// TestPartialRunLeavesEntriesUnderTheirPattern: a run narrowed to one file
// records that file's page. Recorded under the narrow pattern, the entry would
// drop out of the owning run's scope, and that run would never notice the file
// being deleted.
func TestPartialRunLeavesEntriesUnderTheirPattern(t *testing.T) {
	full, dir, store := partialFixture(t)
	a := filepath.Join(dir, "a.md")
	before, ok := tracked(t, store, a)
	require.True(t, ok)

	writeFile(t, dir, "a.md", markdownWithTitle("Page a")+"\nChanged.\n")
	partial := full
	partial.Files = a
	partial.Tracking.Partial = true
	require.NoError(t, Run(context.Background(), partial))

	after, ok := tracked(t, store, a)
	require.True(t, ok)
	assert.Equal(t, before.Glob, after.Glob, "the entry stays under the owning run's pattern")
	assert.NotEqual(t, before.Hash, after.Hash, "the entry still records what was published")

	require.NoError(t, os.Remove(a))
	owner := full
	owner.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), owner))

	_, ok = tracked(t, store, a)
	assert.False(t, ok, "the owning run found the deleted file and dealt with its page")
}

// TestPartialRunRecordsANewFileUnderItsOwnPattern: a file no run has tracked
// has no pattern to keep, so it takes the partial run's until the owning run
// publishes it.
func TestPartialRunRecordsANewFileUnderItsOwnPattern(t *testing.T) {
	full, dir, store := partialFixture(t)
	d := writeFile(t, dir, "d.md", markdownWithTitle("Page d"))

	partial := full
	partial.Files = d
	partial.Tracking.Partial = true
	require.NoError(t, Run(context.Background(), partial))

	entry, ok := tracked(t, store, d)
	require.True(t, ok)
	assert.Equal(t, manifest.Key(d), entry.Glob)

	require.NoError(t, Run(context.Background(), full))

	entry, ok = tracked(t, store, d)
	require.True(t, ok)
	assert.Equal(t, manifest.Key(full.Files), entry.Glob)
}

func TestPartialRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		tracking TrackingConfig
		want     string
	}{
		"without --track-pages": {
			tracking: TrackingConfig{Partial: true},
			want:     "--partial requires --track-pages",
		},
		"with --on-orphan delete": {
			tracking: TrackingConfig{TrackPages: true, Partial: true, OnOrphan: report.OnOrphanDelete},
			want:     "--partial cannot be used with --on-orphan delete",
		},
		"with --on-orphan archive": {
			tracking: TrackingConfig{TrackPages: true, Partial: true, OnOrphan: report.OnOrphanArchive},
			want:     "--partial cannot be used with --on-orphan archive",
		},
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeFile(t, dir, "a.md", markdownWithTitle("Page a"))

			config := trackingConfig(server, filepath.Join(dir, "*.md"))
			config.Tracking = tc.tracking

			err := Run(context.Background(), config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Zero(t, countPagesTitled(t, server, "Page a"), "refused before publishing")
		})
	}
}
