package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// titledDoc is a page document under DOCS > Parent with a body of its own.
func titledDoc(title, body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: " + title + " -->\n\n" + body + "\n"
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
