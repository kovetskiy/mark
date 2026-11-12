package mark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusOf returns the reported page of the file named name.
func statusOf(t *testing.T, pages []report.Page, name string) report.Page {
	t.Helper()

	for _, reported := range pages {
		if filepath.Base(reported.File) == name {
			return reported
		}
	}
	require.Failf(t, "not reported", "%s is not in the report", name)

	return report.Page{}
}

// TestATitleStaysWithTheDocumentItsPageIsRecordedFor: a new document declares
// the title of a page recorded for another document of the run, which keeps
// it. The recorded document publishes to its page and the new one is refused,
// whichever of them sorts first, and the page is written once.
func TestATitleStaysWithTheDocumentItsPageIsRecordedFor(t *testing.T) {
	for _, newcomer := range []string{"a-new.md", "c-new.md"} {
		t.Run(newcomer, func(t *testing.T) {
			server, dir, config, id := overviewPublished(t)
			config.ContinueOnError = true
			version := server.Page(id).Version

			writeFile(t, dir, newcomer, titledDoc("Overview", "New body."))

			for run := 1; run <= 2; run++ {
				pages, err := reportOf(t, config)
				require.Error(t, err)

				assert.Equal(t, id, pageIDTitled(t, server, "Overview"), "run %d", run)
				assert.Contains(t, server.Page(id).Body, "Old body.", "run %d", run)
				assert.Equal(t, version+int64(run), server.Page(id).Version, "run %d: only its own document wrote the page", run)

				require.Len(t, pages, 2, "run %d", run)
				assert.Equal(t, report.StatusPublished, statusOf(t, pages, "b-old.md").Status, "run %d", run)

				refused := statusOf(t, pages, newcomer)
				assert.Equal(t, report.StatusFailed, refused.Status, "run %d", run)
				assert.Contains(t, refused.Reason, "b-old.md already publishes \"Overview\"", "run %d", run)
			}
		})
	}
}

// TestADryRunLeavesATitleWithTheDocumentItsPageIsRecordedFor: the dry run
// refuses the document the run refuses.
func TestADryRunLeavesATitleWithTheDocumentItsPageIsRecordedFor(t *testing.T) {
	server, dir, config, id := overviewPublished(t)
	config.ContinueOnError = true
	config.Mode.DryRun = true
	version := server.Page(id).Version

	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	pages, err := reportOf(t, config)
	require.Error(t, err)

	require.Len(t, pages, 2)
	assert.NotEqual(t, report.StatusFailed, statusOf(t, pages, "b-old.md").Status)

	refused := statusOf(t, pages, "a-new.md")
	assert.Equal(t, report.StatusFailed, refused.Status)
	assert.Contains(t, refused.Reason, "b-old.md already publishes")

	assert.Equal(t, version, server.Page(id).Version, "a dry run writes nothing")
}

// TestATitleStaysWithTheDocumentOfTheKeyItsPageIsRecordedFor: the same for
// documents handed to Run, which the record knows by their Key.
func TestATitleStaysWithTheDocumentOfTheKeyItsPageIsRecordedFor(t *testing.T) {
	server, _ := docsSpace(t)

	recorded := Document{
		Path: "generated/old.md", Key: "old", Content: []byte(titledDoc("Overview", "Old body.")), FS: fstest.MapFS{},
	}
	config := documentsConfig(server, recorded)
	config.DocumentScope = "generated"
	require.NoError(t, Run(context.Background(), config))

	id := pageIDTitled(t, server, "Overview")
	require.NotEmpty(t, id)

	config.Documents = []Document{
		{Path: "generated/new.md", Key: "new", Content: []byte(titledDoc("Overview", "New body.")), FS: fstest.MapFS{}},
		recorded,
	}
	config.ContinueOnError = true

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Contains(t, server.Page(id).Body, "Old body.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusPublished, statusOf(t, pages, "old.md").Status)

	refused := statusOf(t, pages, "new.md")
	assert.Equal(t, report.StatusFailed, refused.Status)
	assert.Contains(t, refused.Reason, "generated/old.md already publishes")
}

// TestARetitleOntoATitleItsDocumentKeepsIsRefused: one document is retitled
// to the title another still declares. The page of that title stays with the
// document it is recorded for, and the retitled document's page keeps the
// title it had.
func TestARetitleOntoATitleItsDocumentKeepsIsRefused(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "b.md", titledDoc("First", "Body of b."))
	writeFile(t, dir, "c.md", titledDoc("Second", "Body of c."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	pageOfB, pageOfC := pageIDTitled(t, server, "First"), pageIDTitled(t, server, "Second")

	writeFile(t, dir, "b.md", titledDoc("Second", "Body of b."))
	config.ContinueOnError = true

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Equal(t, "First", server.Page(pageOfB).Title)
	assert.Equal(t, "Second", server.Page(pageOfC).Title)
	assert.Contains(t, server.Page(pageOfC).Body, "Body of c.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusPublished, statusOf(t, pages, "c.md").Status)
	assert.Contains(t, statusOf(t, pages, "b.md").Reason, "c.md already publishes")
}

// TestATitleOfARecordedPageLeftAloneIsNotTakenOver: --no-overwrite leaves the
// page edited in Confluence as it is, and it is still its document's.
func TestATitleOfARecordedPageLeftAloneIsNotTakenOver(t *testing.T) {
	server, dir, config, id := overviewPublished(t)
	config.Tracking.NoOverwrite = true
	config.ContinueOnError = true

	server.EditPage(id, "<p>Written by a person.</p>")
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Contains(t, server.Page(id).Body, "Written by a person.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusSkipped, statusOf(t, pages, "b-old.md").Status)
	assert.Equal(t, report.StatusFailed, statusOf(t, pages, "a-new.md").Status)
}

// TestARenamedFileKeepsItsPageBesideANewDocumentOfItsTitle: the file the page
// is recorded for is gone, and its content is in the run under another name.
// That document is the recorded one renamed, so the page is still its own.
func TestARenamedFileKeepsItsPageBesideANewDocumentOfItsTitle(t *testing.T) {
	server, dir, config, id := overviewPublished(t)
	config.ContinueOnError = true

	require.NoError(t, os.Rename(filepath.Join(dir, "b-old.md"), filepath.Join(dir, "c-renamed.md")))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Equal(t, id, pageIDTitled(t, server, "Overview"))
	assert.Contains(t, server.Page(id).Body, "Old body.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusPublished, statusOf(t, pages, "c-renamed.md").Status)
	assert.Contains(t, statusOf(t, pages, "a-new.md").Reason, "c-renamed.md already publishes")
}

// TestARenamedFileOfAnUnchangedTitleTakesOverItsPage: a file renamed with its
// title kept finds its page by the title, edited or not, and is not refused
// for the page being recorded under the name it had.
func TestARenamedFileOfAnUnchangedTitleTakesOverItsPage(t *testing.T) {
	for name, body := range map[string]string{"same content": "Old body.", "edited": "Edited body."} {
		t.Run(name, func(t *testing.T) {
			server, dir, config, id := overviewPublished(t)

			require.NoError(t, os.Remove(filepath.Join(dir, "b-old.md")))
			writeFile(t, dir, "a-renamed.md", titledDoc("Overview", body))

			for run := 1; run <= 2; run++ {
				require.NoError(t, Run(context.Background(), config), "run %d", run)

				assert.Equal(t, id, pageIDTitled(t, server, "Overview"), "run %d", run)
				assert.Contains(t, server.Page(id).Body, body, "run %d", run)
			}
		})
	}
}

// TestATitleOfADocumentGoneFromTheRunGoesToTheOneTakingIt: the file the page
// is recorded for was deleted, so nothing of the run owns the page, and the
// first document declaring its title publishes to it.
func TestATitleOfADocumentGoneFromTheRunGoesToTheOneTakingIt(t *testing.T) {
	server, dir, config, id := overviewPublished(t)
	config.ContinueOnError = true

	require.NoError(t, os.Remove(filepath.Join(dir, "b-old.md")))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "Body of a."))
	writeFile(t, dir, "c-new.md", titledDoc("Overview", "Body of c."))

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Equal(t, id, pageIDTitled(t, server, "Overview"))
	assert.Contains(t, server.Page(id).Body, "Body of a.")

	require.Len(t, pages, 2)
	assert.Contains(t, statusOf(t, pages, "c-new.md").Reason, "a-new.md already publishes")
}

// TestWithoutTrackingATitleGoesToTheFirstDocumentDeclaringIt: nothing records
// which document published the page, so the first of the run takes it.
func TestWithoutTrackingATitleGoesToTheFirstDocumentDeclaringIt(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "b-old.md", titledDoc("Overview", "Old body."))

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	id := pageIDTitled(t, server, "Overview")
	require.NotEmpty(t, id)

	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))
	config.ContinueOnError = true

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Contains(t, server.Page(id).Body, "New body.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusPublished, statusOf(t, pages, "a-new.md").Status)
	assert.True(t, strings.Contains(statusOf(t, pages, "b-old.md").Reason, "a-new.md already publishes"))
}
