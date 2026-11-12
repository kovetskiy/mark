package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// titledDoc is a page document under DOCS > Parent with a body of its own.
func titledDoc(title, body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: " + title + " -->\n\n" + body + "\n"
}

// overviewPublished publishes b-old.md as "Overview" with tracking on, and
// returns the directory, the run's config and the page's id.
func overviewPublished(t *testing.T) (*confluencetest.Server, string, Config, string) {
	t.Helper()

	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "b-old.md", titledDoc("Overview", "Old body."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	id := pageIDTitled(t, server, "Overview")
	require.NotEmpty(t, id)

	return server, dir, config, id
}

// reportOf runs config and returns the pages of its JSON report and the
// run's error.
func reportOf(t *testing.T, config Config) ([]report.Page, error) {
	t.Helper()

	var out bytes.Buffer
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out

	err := Run(context.Background(), config)

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))

	return parsed.Pages, err
}

// TestATitleFreedByARetitleGoesToTheDocumentTakingIt: one document gives up
// its title in the run in which another, sorting first, takes it. The page
// follows the document that published it, and the title's new document gets
// a page of its own.
func TestATitleFreedByARetitleGoesToTheDocumentTakingIt(t *testing.T) {
	server, dir, config, legacyID := overviewPublished(t)

	writeFile(t, dir, "b-old.md", titledDoc("Legacy overview", "Old body."))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	for run := 1; run <= 2; run++ {
		pages, err := reportOf(t, config)
		require.NoError(t, err)

		assert.Equal(t, "Legacy overview", server.Page(legacyID).Title, "run %d", run)
		assert.Contains(t, server.Page(legacyID).Body, "Old body.", "run %d", run)

		overviewID := pageIDTitled(t, server, "Overview")
		require.NotEmpty(t, overviewID, "run %d: the title has a page", run)
		assert.NotEqual(t, legacyID, overviewID, "run %d", run)
		assert.Contains(t, server.Page(overviewID).Body, "New body.", "run %d", run)

		require.Len(t, pages, 2, "run %d: each document is reported once", run)
		assert.NotEqual(t, pages[0].PageID, pages[1].PageID, "run %d", run)
	}
}

// TestADryRunSaysATitleFreedByARetitleGetsANewPage: the dry run reports what
// the run does.
func TestADryRunSaysATitleFreedByARetitleGetsANewPage(t *testing.T) {
	_, dir, config, _ := overviewPublished(t)

	writeFile(t, dir, "b-old.md", titledDoc("Legacy overview", "Old body."))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	config.Mode.DryRun = true
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), config))
	})

	assert.True(t, strings.Contains(logged, "a-new.md would be published as a new page"),
		"the document taking the title is previewed as a new page")
}

// TestATitleItsPageCouldNotGiveUpIsNotTakenOver: the document retitling the
// page fails, so the page still has the title, and the document taking the
// title is refused rather than published over it.
func TestATitleItsPageCouldNotGiveUpIsNotTakenOver(t *testing.T) {
	server, dir, config, legacyID := overviewPublished(t)

	// A parent title longer than Confluence holds fails the document before
	// its page is touched.
	writeFile(t, dir, "b-old.md", strings.Replace(titledDoc("Legacy overview", "Old body."),
		"<!-- Parent: Parent -->", "<!-- Parent: "+strings.Repeat("p", 300)+" -->", 1))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))
	config.ContinueOnError = true

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Equal(t, "Overview", server.Page(legacyID).Title)
	assert.Contains(t, server.Page(legacyID).Body, "Old body.")

	require.Len(t, pages, 2)
	for _, reported := range pages {
		assert.Equal(t, report.StatusFailed, reported.Status, reported.File)
	}
	for _, reported := range pages {
		if strings.HasSuffix(reported.File, "a-new.md") {
			assert.Contains(t, reported.Reason, "b-old.md", "the document refused names the one its title waits on")
		}
	}
}

// TestATitleOfAPageLeftAloneIsNotTakenOver: --no-overwrite leaves the page
// edited in Confluence as it is, title included, so the document taking the
// title is refused rather than published over the edit.
func TestATitleOfAPageLeftAloneIsNotTakenOver(t *testing.T) {
	server, dir, config, legacyID := overviewPublished(t)
	config.Tracking.NoOverwrite = true
	config.ContinueOnError = true

	server.EditPage(legacyID, "<p>Written by a person.</p>")

	writeFile(t, dir, "b-old.md", titledDoc("Legacy overview", "Old body."))
	writeFile(t, dir, "a-new.md", titledDoc("Overview", "New body."))

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Equal(t, "Overview", server.Page(legacyID).Title)
	assert.Contains(t, server.Page(legacyID).Body, "Written by a person.")

	require.Len(t, pages, 2)
	for _, reported := range pages {
		if strings.HasSuffix(reported.File, "a-new.md") {
			assert.Equal(t, report.StatusFailed, reported.Status)
		} else {
			assert.Equal(t, report.StatusSkipped, reported.Status)
		}
	}
}

// TestTwoDocumentsSwappingTitlesBothPublish: each takes the title the other
// gives up, so neither can go first; both still publish, as they did.
func TestTwoDocumentsSwappingTitlesBothPublish(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", titledDoc("First", "Body of a."))
	writeFile(t, dir, "b.md", titledDoc("Second", "Body of b."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	writeFile(t, dir, "a.md", titledDoc("Second", "Body of a."))
	writeFile(t, dir, "b.md", titledDoc("First", "Body of b."))
	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Second"), "Body of a.")
	assert.Contains(t, bodyOfPageTitled(t, server, "First"), "Body of b.")
}

// TestADryRunOfSwappedTitlesPreviewsThePagesTheTitlesFind: neither title is
// freed, so neither document is previewed as a new page.
func TestADryRunOfSwappedTitlesPreviewsThePagesTheTitlesFind(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", titledDoc("First", "Body of a."))
	writeFile(t, dir, "b.md", titledDoc("Second", "Body of b."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	writeFile(t, dir, "a.md", titledDoc("Second", "Body of a."))
	writeFile(t, dir, "b.md", titledDoc("First", "Body of b."))

	config.Mode.DryRun = true
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), config))
	})

	assert.False(t, strings.Contains(logged, "would be published as a new page"))
}

// TestTitlesHandedDownAChainEachKeepTheirPage: a takes the title b gives up
// and b the one c gives up, so c is published first, then b, then a, each to
// the page it published before, and a to a new one.
func TestTitlesHandedDownAChainEachKeepTheirPage(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "b.md", titledDoc("First", "Body of b."))
	writeFile(t, dir, "c.md", titledDoc("Second", "Body of c."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	pageOfB, pageOfC := pageIDTitled(t, server, "First"), pageIDTitled(t, server, "Second")

	writeFile(t, dir, "a.md", titledDoc("First", "Body of a."))
	writeFile(t, dir, "b.md", titledDoc("Second", "Body of b."))
	writeFile(t, dir, "c.md", titledDoc("Third", "Body of c."))

	pages, err := reportOf(t, config)
	require.NoError(t, err)
	require.Len(t, pages, 3)

	assert.Equal(t, "Second", server.Page(pageOfB).Title)
	assert.Contains(t, server.Page(pageOfB).Body, "Body of b.")
	assert.Equal(t, "Third", server.Page(pageOfC).Title)
	assert.Contains(t, server.Page(pageOfC).Body, "Body of c.")
	assert.Contains(t, bodyOfPageTitled(t, server, "First"), "Body of a.")
	assert.NotEqual(t, pageOfB, pageIDTitled(t, server, "First"))
}

// TestATitleOfADocumentLeftOutOfTheRunIsStillFoundByTitle: a page recorded
// for a document the run does not publish is found by its title as before.
func TestATitleOfADocumentLeftOutOfTheRunIsStillFoundByTitle(t *testing.T) {
	server, _, config, id := overviewPublished(t)

	// b-old.md still names the title, in a directory this run does not publish.
	file := writeFile(t, t.TempDir(), "other.md", titledDoc("Overview", "New body."))
	config.Files = file
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, id, pageIDTitled(t, server, "Overview"))
	assert.Contains(t, server.Page(id).Body, "New body.")
}

// TestTwoDocumentsOfOneTitleAreRefused: a space holds one page of a title, so
// the second document of a run to name it is refused whether or not parents
// come from the path, and the first keeps the page.
func TestTwoDocumentsOfOneTitleAreRefused(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "one.md", titledDoc("Shared", "Body of one."))
	writeFile(t, dir, "two.md", titledDoc("shared", "Body of two."))

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.ContinueOnError = true

	pages, err := reportOf(t, config)
	require.Error(t, err)

	assert.Contains(t, bodyOfPageTitled(t, server, "Shared"), "Body of one.")

	require.Len(t, pages, 2)
	assert.Equal(t, report.StatusPublished, pages[0].Status)
	assert.Equal(t, report.StatusFailed, pages[1].Status)
	assert.Contains(t, pages[1].Reason, "one.md already publishes")
}

// TestAPublisherRefusesASecondFileOfOneTitle: a library caller publishing
// file by file is refused the same way.
func TestAPublisherRefusesASecondFileOfOneTitle(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	one := writeFile(t, dir, "one.md", titledDoc("Shared", "Body of one."))
	two := writeFile(t, dir, "two.md", titledDoc("Shared", "Body of two."))

	ctx := context.Background()
	publisher, err := NewPublisher(ctx, api, publishConfig(server.URL, "").PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(ctx, one)
	require.NoError(t, err)

	_, err = publisher.Publish(ctx, two)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already publishes")
	assert.Contains(t, bodyOfPageTitled(t, server, "Shared"), "Body of one.")

	_, err = publisher.Publish(ctx, one)
	require.NoError(t, err, "a file published again keeps its title")
}

// TestDocumentsOfOneTitleThatDoNotMeetStillPublish: a page and a blog post
// of one title are two pieces of content, and a document that is not
// synchronized publishes nothing.
func TestDocumentsOfOneTitleThatDoNotMeetStillPublish(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a-draft.md",
		"<!-- Space: DOCS -->\n<!-- Title: Shared -->\n<!-- Synchronized: false -->\n\nDraft.\n")
	writeFile(t, dir, "b-page.md", titledDoc("Shared", "Body of the page."))
	writeFile(t, dir, "c-post.md",
		"<!-- Space: DOCS -->\n<!-- Type: blogpost -->\n<!-- Title: Shared -->\n\nBody of the post.\n")

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Shared"), "Body of the page.")
}

// TestATitleChangedOnlyInCaseIsRetitled: the title lookup ignores case, so
// the page is found; it still takes the title as the document now writes it,
// also when the body has not changed.
func TestATitleChangedOnlyInCaseIsRetitled(t *testing.T) {
	for name, changesOnly := range map[string]bool{"every run writes": false, "changes only": true} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", titledDoc("Api guide", "Body."))

			config := publishConfig(server.URL, file)
			config.Update.ChangesOnly = changesOnly
			require.NoError(t, Run(context.Background(), config))

			id := pageIDTitled(t, server, "Api guide")
			require.NotEmpty(t, id)

			writeFile(t, dir, "doc.md", titledDoc("API Guide", "Body."))
			require.NoError(t, Run(context.Background(), config))

			assert.Equal(t, "API Guide", server.Page(id).Title)

			version := server.Page(id).Version
			require.NoError(t, Run(context.Background(), config))
			if changesOnly {
				assert.Equal(t, version, server.Page(id).Version, "a title already as written is not written again")
			}
		})
	}
}

// TestAPublisherKnowsAFileHoweverItIsSpelled: one file named two ways is one
// document, and keeps its title.
func TestAPublisherKnowsAFileHoweverItIsSpelled(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	one := writeFile(t, dir, "one.md", titledDoc("Shared", "Body of one."))

	ctx := context.Background()
	publisher, err := NewPublisher(ctx, api, publishConfig(server.URL, "").PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(ctx, one)
	require.NoError(t, err)

	_, err = publisher.Publish(ctx, dir+string(filepath.Separator)+"."+string(filepath.Separator)+"one.md")
	require.NoError(t, err)
}

// TestAPublisherLeavesATitleAFileGaveUpToAnother: a file published again
// under a new title holds that one, and the title it had goes to the next
// file naming it. A file renamed is another file, and is refused its title.
func TestAPublisherLeavesATitleAFileGaveUpToAnother(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	one := writeFile(t, dir, "one.md", titledDoc("First", "Body of one."))

	ctx := context.Background()
	publisher, err := NewPublisher(ctx, api, publishConfig(server.URL, "").PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(ctx, one)
	require.NoError(t, err)

	writeFile(t, dir, "one.md", titledDoc("Second", "Body of one."))
	_, err = publisher.Publish(ctx, one)
	require.NoError(t, err)

	two := writeFile(t, dir, "two.md", titledDoc("First", "Body of two."))
	_, err = publisher.Publish(ctx, two)
	require.NoError(t, err, "the title one.md gave up is free")
	assert.Contains(t, bodyOfPageTitled(t, server, "First"), "Body of two.")

	three := writeFile(t, dir, "three.md", titledDoc("Second", "Body of three."))
	_, err = publisher.Publish(ctx, three)
	require.Error(t, err, "one.md holds the title it was published under last")

	renamed := filepath.Join(dir, "renamed.md")
	require.NoError(t, os.Rename(one, renamed))
	_, err = publisher.Publish(ctx, renamed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one.md already publishes")
}
