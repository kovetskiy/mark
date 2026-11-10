package mark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// partialFixture publishes a.md, b.md and c.md under one pattern with
// --track-pages, as the run that owns them does.
func partialFixture(t *testing.T) (*confluencetest.Server, Config, string) {
	t.Helper()

	server, _ := docsSpace(t)
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeFile(t, dir, name+".md", markdownWithTitle("Page "+name))
	}

	full := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), full))

	return server, full, dir
}

// partialOf is the run a job publishing only some of full's files makes.
func partialOf(full Config, dir, files string) Config {
	partial := full
	partial.Files = filepath.Join(dir, files)
	partial.Tracking.Partial = true

	return partial
}

// tracked reads the manifest afresh: a store reads it once and keeps it.
func tracked(t *testing.T, server *confluencetest.Server, file string) (manifest.Entry, bool) {
	t.Helper()

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	entry, ok, err := manifest.NewStore(api).Lookup(context.Background(), "DOCS", file)
	require.NoError(t, err)

	return entry, ok
}

// TestPartialRunReportsOnlyWhatItNames: a job given the paths a change
// touched, a deleted one among them, reports that one as gone and nothing
// else. Without --partial its pattern matches no entry, the owning run's
// pattern having recorded them all, so the deleted file is not reported, and
// with the owning run's pattern every file it left out would be.
func TestPartialRunReportsOnlyWhatItNames(t *testing.T) {
	server, full, dir := partialFixture(t)
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialOf(full, dir, "{a,b}.md")))
	})

	reported := ""
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, "had no matching source file") {
			reported = line
		}
	}
	assert.Contains(t, reported, "b.md")
	assert.NotContains(t, reported, "c.md", "c.md is not this run's business")

	_, ok := tracked(t, server, filepath.Join(dir, "c.md"))
	assert.True(t, ok, "c.md is still tracked")

	// Reporting leaves b.md to the run owning it, which deals with its page.
	_, ok = tracked(t, server, filepath.Join(dir, "b.md"))
	require.True(t, ok, "b.md was only reported, so still tracked")
	bID := pageIDTitled(t, server, "Page b")
	owner := full
	owner.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), owner))
	assert.True(t, server.Page(bID).Trashed)
}

// TestPartialRunActsOnlyOnWhatItNames: --on-orphan in a partial run removes
// the page of a file the run names and finds gone, and no other.
func TestPartialRunActsOnlyOnWhatItNames(t *testing.T) {
	server, full, dir := partialFixture(t)
	bID := pageIDTitled(t, server, "Page b")
	cID := pageIDTitled(t, server, "Page c")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	partial := partialOf(full, dir, "{a,b}.md")
	partial.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), partial))

	assert.True(t, server.Page(bID).Trashed, "b.md was named and is gone")
	assert.False(t, server.Page(cID).Trashed, "c.md was not named, and is left for the owning run")
	_, ok := tracked(t, server, filepath.Join(dir, "c.md"))
	assert.True(t, ok)
}

// TestPartialRunGivenTheOwningPatternRemovesNothing: the owning run's own
// pattern with --partial, on a checkout holding only some of its files, reads
// every other file as deleted; it reports them and leaves their pages alone.
func TestPartialRunGivenTheOwningPatternRemovesNothing(t *testing.T) {
	server, full, dir := partialFixture(t)
	bID := pageIDTitled(t, server, "Page b")
	cID := pageIDTitled(t, server, "Page c")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	sparse := partialOf(full, dir, "*.md")
	sparse.Tracking.OnOrphan = report.OnOrphanDelete
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), sparse))
	})
	assert.Contains(t, logged, "--partial was given the pattern that recorded it")

	for _, id := range []string{bID, cID} {
		assert.False(t, server.Page(id).Trashed)
	}
	for _, name := range []string{"b.md", "c.md"} {
		_, ok := tracked(t, server, filepath.Join(dir, name))
		assert.True(t, ok, "%s stays tracked for the owning run", name)
	}
}

// TestPartialRunLeavesEntriesUnderTheirPattern: a run narrowed to one file
// records that file's page. Recorded under the narrow pattern, the entry would
// drop out of the owning run's scope, and that run would never notice the file
// being deleted.
func TestPartialRunLeavesEntriesUnderTheirPattern(t *testing.T) {
	server, full, dir := partialFixture(t)
	a := filepath.Join(dir, "a.md")
	before, ok := tracked(t, server, a)
	require.True(t, ok)

	writeFile(t, dir, "a.md", markdownWithTitle("Page a")+"\nChanged.\n")
	require.NoError(t, Run(context.Background(), partialOf(full, dir, "a.md")))

	after, ok := tracked(t, server, a)
	require.True(t, ok)
	assert.Equal(t, before.Glob, after.Glob, "the entry stays under the owning run's pattern")
	assert.NotEqual(t, before.Hash, after.Hash, "the entry still records what was published")

	require.NoError(t, os.Remove(a))
	owner := full
	owner.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), owner))

	_, ok = tracked(t, server, a)
	assert.False(t, ok, "the owning run found the deleted file and dealt with its page")
}

// TestPartialRunRecordsANewFileUnderItsOwnPattern: a file no run has tracked
// has no pattern to keep, so it takes the partial run's until the owning run
// publishes it.
func TestPartialRunRecordsANewFileUnderItsOwnPattern(t *testing.T) {
	server, full, dir := partialFixture(t)
	d := writeFile(t, dir, "d.md", markdownWithTitle("Page d"))

	require.NoError(t, Run(context.Background(), partialOf(full, dir, "d.md")))

	entry, ok := tracked(t, server, d)
	require.True(t, ok)
	assert.Equal(t, manifest.Key(d), entry.Glob)

	require.NoError(t, Run(context.Background(), full))

	entry, ok = tracked(t, server, d)
	require.True(t, ok)
	assert.Equal(t, manifest.Key(full.Files), entry.Glob)
}

// TestPartialRunFollowsAMove: a file moved with its title unchanged, given to a
// partial run under both names, hands its entry to the new path. Without
// --partial the old entry belongs to another pattern, so the run warns that two
// files claim the page, keeps both, and the new path starts under the partial
// pattern.
func TestPartialRunFollowsAMove(t *testing.T) {
	server, full, dir := partialFixture(t)
	b := filepath.Join(dir, "b.md")
	before, ok := tracked(t, server, b)
	require.True(t, ok)

	moved := filepath.Join(dir, "moved.md")
	require.NoError(t, os.Rename(b, moved))

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialOf(full, dir, "{b,moved}.md")))
	})
	assert.NotContains(t, logged, "both publish to page")

	_, ok = tracked(t, server, b)
	assert.False(t, ok, "the old path handed its entry over")
	after, ok := tracked(t, server, moved)
	require.True(t, ok)
	assert.Equal(t, before.PageID, after.PageID)
	assert.Equal(t, before.Glob, after.Glob, "the new path takes the old one's pattern")
}

// TestPartialRunMovesAFileIntoAnotherJobsPattern: a file moved out of one
// job's pattern into another's does not keep the first job's pattern, or that
// job's next run would read it as deleted and trash the page it still
// publishes to.
func TestPartialRunMovesAFileIntoAnotherJobsPattern(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	for _, sub := range []string{"docs", "guides"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, sub), 0o700))
	}
	writeFile(t, filepath.Join(dir, "docs"), "keep.md", markdownWithTitle("Keep"))
	writeFile(t, filepath.Join(dir, "docs"), "x.md", markdownWithTitle("Page x"))
	writeFile(t, filepath.Join(dir, "guides"), "y.md", markdownWithTitle("Page y"))

	docs := trackingConfig(server, filepath.Join(dir, "docs", "*.md"))
	guides := trackingConfig(server, filepath.Join(dir, "guides", "*.md"))
	require.NoError(t, Run(context.Background(), docs))
	require.NoError(t, Run(context.Background(), guides))
	id := pageIDTitled(t, server, "Page x")

	moved := filepath.Join(dir, "guides", "x.md")
	require.NoError(t, os.Rename(filepath.Join(dir, "docs", "x.md"), moved))
	require.NoError(t, Run(context.Background(), partialOf(docs, dir, "{docs,guides}/x.md")))

	entry, ok := tracked(t, server, moved)
	require.True(t, ok)
	assert.Equal(t, id, entry.PageID, "the move was followed")
	assert.NotEqual(t, manifest.Key(docs.Files), entry.Glob)

	docs.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), docs))
	assert.False(t, server.Page(id).Trashed, "guides/x.md still publishes to it")
}

// TestPartialRunNamesTheMissingPathOfAMove: a partial run given only the new
// path of a moved file cannot tell a move from two files claiming one page,
// and its warning says which path to name.
func TestPartialRunNamesTheMissingPathOfAMove(t *testing.T) {
	_, full, dir := partialFixture(t)
	require.NoError(t, os.Rename(filepath.Join(dir, "b.md"), filepath.Join(dir, "moved.md")))

	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), partialOf(full, dir, "moved.md")))
	})
	assert.Contains(t, logged, "both publish to page")
	assert.Contains(t, logged, "name both paths in --files to follow the move")
}

// TestPartialRunFollowsARename: a file renamed along with its title is found
// by its content, as in a full run, and an edit made in Confluence survives
// it under --no-overwrite.
func TestPartialRunFollowsARename(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "old-name.md", markdownTitledFromFilename())
	writeFile(t, dir, "other.md", markdownTitledFromFilename()+"\nOther.\n")

	full := filenameTitleConfig(server, filepath.Join(dir, "*.md"))
	full.Tracking.NoOverwrite = true
	require.NoError(t, Run(context.Background(), full))

	id := pageIDTitled(t, server, "Old Name")
	server.EditPage(id, "<p>Written by a person.</p>")
	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))

	renamed := runReported(t, partialOf(full, dir, "{old-name,new-name}.md"))
	assert.Equal(t, report.StatusSkipped, renamed.Status)
	assert.Contains(t, renamed.Reason, "edited in Confluence")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)
	assert.Zero(t, countPagesTitled(t, server, "New Name"), "no second page")

	entry, ok := tracked(t, server, filepath.Join(dir, "new-name.md"))
	require.True(t, ok)
	assert.Equal(t, id, entry.PageID)
	assert.Equal(t, manifest.Key(full.Files), entry.Glob)
}

func TestPartialRequiresTrackPages(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", markdownWithTitle("Page a"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking = TrackingConfig{Partial: true}

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--partial requires --track-pages")
	assert.Zero(t, countPagesTitled(t, server, "Page a"), "refused before publishing")
}
