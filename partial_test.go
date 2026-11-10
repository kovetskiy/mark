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

	partial := partialOf(full, dir, "")
	partial.Files = ""
	partial.Paths = []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")}
	partial.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), partial))

	assert.True(t, server.Page(bID).Trashed, "b.md was named and is gone")
	assert.False(t, server.Page(cID).Trashed, "c.md was not named, and is left for the owning run")
	_, ok := tracked(t, server, filepath.Join(dir, "c.md"))
	assert.True(t, ok)
}

// TestPartialRunGivenTheOwningPatternLeavesItsFilesAlone: the owning run's own
// pattern with --partial, on a checkout holding only some of its files, would
// read every other file as deleted. Like a page outside --orphan-under they are
// not reported -- in the github format every one would be a warning on every
// such run -- and stay tracked for the owning run. A path named exactly beside
// the pattern is still reported.
func TestPartialRunGivenTheOwningPatternLeavesItsFilesAlone(t *testing.T) {
	server, full, dir := partialFixture(t)
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	var out strings.Builder
	sparse := partialOf(full, dir, "*.md")
	sparse.Paths = []string{filepath.Join(dir, "b.md")}
	sparse.Report.OutputFormat = report.FormatGitHub
	sparse.Output = &out
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), sparse))
	})
	assert.Contains(t, logged, "the pattern given to --partial")

	var warnings []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "::warning") {
			warnings = append(warnings, line)
		}
	}
	require.Len(t, warnings, 1, out.String())
	assert.Contains(t, warnings[0], "b.md", "named exactly, so reported")

	for _, name := range []string{"b.md", "c.md"} {
		_, ok := tracked(t, server, filepath.Join(dir, name))
		assert.True(t, ok, "%s stays tracked for the owning run", name)
	}
}

// TestPartialRunRemovingRefusesAPattern: a wildcard under --partial reaches
// every tracked file it matches, another job's included, and on a checkout
// holding only some of them reads the rest as deleted. Told to archive or
// delete, a partial run takes only exact paths, and says so before publishing.
func TestPartialRunRemovingRefusesAPattern(t *testing.T) {
	server, full, dir := partialFixture(t)
	writeFile(t, dir, "a.md", markdownWithTitle("Page a")+"\nChanged.\n")

	for _, action := range []report.OnOrphan{report.OnOrphanArchive, report.OnOrphanDelete} {
		for _, files := range []string{"*.md", "{a,b}.md", "[ab].md", "a?md"} {
			partial := partialOf(full, dir, files)
			partial.Tracking.OnOrphan = action

			err := Run(context.Background(), partial)
			require.ErrorIs(t, err, ErrPartialPattern, files)
			for _, name := range []string{
				"Tracking.OnOrphan", "--on-orphan", "Tracking.Partial", "--partial",
				"Files", "--files", "Paths", "--files-from",
			} {
				assert.ErrorContains(t, err, name)
			}
		}
	}

	aID := pageIDTitled(t, server, "Page a")
	assert.NotContains(t, server.Page(aID).Body, "Changed", "refused before publishing")

	// Reporting removes nothing, so a pattern is fine.
	reporting := partialOf(full, dir, "*.md")
	require.NoError(t, Run(context.Background(), reporting))
}

// TestPartialRunRemovingTakesOneFile: Files spelling one file, an escaped glob
// character included, names exactly that file, as Paths would.
func TestPartialRunRemovingTakesOneFile(t *testing.T) {
	server, full, dir := partialFixture(t)
	bID := pageIDTitled(t, server, "Page b")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	writeFile(t, dir, "[x].md", markdownWithTitle("Page x"))

	for _, files := range []string{"b.md", `\[x\].md`} {
		partial := partialOf(full, dir, files)
		partial.Tracking.OnOrphan = report.OnOrphanDelete
		err := Run(context.Background(), partial)
		if files == "b.md" {
			// Nothing to publish: the change only deleted b.md.
			require.ErrorContains(t, err, "no files matched")
			continue
		}
		require.NoError(t, err)
	}
	assert.Equal(t, 1, countPagesTitled(t, server, "Page x"))

	partial := full
	partial.Files = ""
	partial.Paths = []string{filepath.Join(dir, "b.md"), filepath.Join(dir, "a.md")}
	partial.Tracking.Partial = true
	partial.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), partial))
	assert.True(t, server.Page(bID).Trashed)
}

// TestPartialRunStillWarnsOfAKeptOrphan: a page --on-orphan was asked to remove
// and left alone, here for holding a child, is still a warning in the github
// format under --partial.
func TestPartialRunStillWarnsOfAKeptOrphan(t *testing.T) {
	server, full, dir := partialFixture(t)
	bID := pageIDTitled(t, server, "Page b")
	server.AddPage("DOCS", "Child of b", "page", bID)
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))

	var out strings.Builder
	partial := full
	partial.Files = ""
	partial.Paths = []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")}
	partial.Tracking.Partial = true
	partial.Tracking.OnOrphan = report.OnOrphanDelete
	partial.Report.OutputFormat = report.FormatGitHub
	partial.Output = &out
	require.NoError(t, Run(context.Background(), partial))

	assert.False(t, server.Page(bID).Trashed)
	assert.Contains(t, out.String(), "::warning")
	assert.Contains(t, out.String(), "left alone")
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
	assert.Contains(t, logged, "name both paths in --files or --files-from to follow the move")
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

// TestPartialRunGivenPaths: paths given as a list are taken as written, glob
// characters and all, and a deleted one among them is looked for like a file
// a pattern names.
func TestPartialRunGivenPaths(t *testing.T) {
	server, full, dir := partialFixture(t)
	weird := writeFile(t, dir, "we[a]{b,c}*.md", markdownWithTitle("Page weird"))
	require.NoError(t, Run(context.Background(), full))
	bID := pageIDTitled(t, server, "Page b")
	cID := pageIDTitled(t, server, "Page c")
	require.NoError(t, os.Remove(filepath.Join(dir, "b.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "c.md")))

	writeFile(t, dir, "we[a]{b,c}*.md", markdownWithTitle("Page weird renamed"))
	partial := full
	partial.Files = ""
	partial.Paths = []string{weird, filepath.Join(dir, "b.md")}
	partial.Tracking.Partial = true
	partial.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), partial))

	assert.Equal(t, 1, countPagesTitled(t, server, "Page weird renamed"), "the path was published as written")
	assert.Equal(t, 1, countPagesTitled(t, server, "Page a"))
	assert.True(t, server.Page(bID).Trashed, "b.md was named and is gone")
	assert.False(t, server.Page(cID).Trashed, "c.md was not named")

	entry, ok := tracked(t, server, weird)
	require.True(t, ok)
	assert.Equal(t, manifest.Key(full.Files), entry.Glob, "the entry keeps the owning run's pattern")
}

// TestPathsAreNotPatterns: a path holding glob characters names that one file,
// not the files it would match as a pattern.
func TestPathsAreNotPatterns(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	literal := writeFile(t, dir, "[ab].md", markdownWithTitle("Literal"))
	writeFile(t, dir, "a.md", markdownWithTitle("Page a"))

	config := trackingConfig(server, "")
	config.Paths = []string{literal}
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, 1, countPagesTitled(t, server, "Literal"))
	assert.Zero(t, countPagesTitled(t, server, "Page a"))
}

func TestPathsWithParentsFromPathNeedARoot(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	config := trackingConfig(server, "")
	config.Paths = []string{writeFile(t, dir, "a.md", markdownWithTitle("Page a"))}
	config.ParentsFromPath = true

	err := Run(context.Background(), config)
	require.ErrorContains(t, err, "needs --parents-from-path-root")
	assert.Zero(t, countPagesTitled(t, server, "Page a"))
}
