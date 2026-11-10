package mark

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionalManifestConfig tracks pages in dir on the page titled Handbook,
// which may be missing, and reports as JSON into out.
func optionalManifestConfig(server *confluencetest.Server, dir string, out io.Writer) Config {
	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Output = out
	config.Tracking.ManifestPage = "Handbook"
	config.Tracking.ManifestPageOptional = true
	config.Report.OutputFormat = report.FormatJSON
	return config
}

// parseReport reads the JSON report out of a run's output, past the bodies a
// dry run prints ahead of it.
func parseReport(t *testing.T, out *bytes.Buffer) *report.Report {
	t.Helper()

	data := out.Bytes()
	if start := bytes.LastIndex(data, []byte("\n{\n")); start >= 0 {
		data = data[start+1:]
	}

	return decodeReport(t, data)
}

// TestManifestPageMissingFailsTheRun: without ManifestPageOptional a missing
// manifest page fails the run, and a caller can tell that apart from any
// other failure.
func TestManifestPageMissingFailsTheRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := optionalManifestConfig(server, dir, io.Discard)
	config.Tracking.ManifestPageOptional = false

	err := Run(context.Background(), config)
	require.Error(t, err)

	var missing *manifest.MissingPageError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "Handbook", missing.Page)
	assert.Equal(t, "DOCS", missing.Space)

	assert.Empty(t, pageIDTitled(t, server, "Guide"), "refused before the document was published")
}

// TestManifestPageOptionalPublishesUntracked is the first run into a new
// space: the manifest page does not exist yet, so the documents publish
// untracked, nothing is written where a manifest would be, and the report says
// which space ran so. A dry run says the same.
func TestManifestPageOptionalPublishesUntracked(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	var dry bytes.Buffer
	config := optionalManifestConfig(server, dir, &dry)
	config.Mode.DryRun = true
	require.NoError(t, Run(context.Background(), config))
	assert.Equal(t,
		[]report.Untracked{{Space: "DOCS", ManifestPage: "Handbook"}},
		parseReport(t, &dry).Untracked, "a dry run reports it too")

	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), optionalManifestConfig(server, dir, &out)))

	parsed := parseReport(t, &out)
	assert.Equal(t, []report.Untracked{{Space: "DOCS", ManifestPage: "Handbook"}}, parsed.Untracked)
	require.Len(t, parsed.Pages, 1)
	assert.Equal(t, report.StatusPublished, parsed.Pages[0].Status)

	mustFind(t, server, "Guide")

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	spaceID, err := api.GetSpaceID(context.Background(), "DOCS")
	require.NoError(t, err)
	assert.Equal(t, 0, manifestShards(server, spaceID), "no manifest is kept anywhere else")
}

// TestManifestPageOptionalLandingPagePublishedLater is the case behind the
// option: the documents publish before the landing page that holds their
// manifest, in the same run. The mapping is saved on the landing page once it
// exists, so the run is tracked after all, and the next run finds a retitled
// document's page.
func TestManifestPageOptionalLandingPagePublishedLater(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a-guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")
	writeFile(t, dir, "z-handbook.md", "<!-- Space: DOCS -->\n<!-- Title: Handbook -->\n\nThe handbook.\n")

	var out bytes.Buffer
	config := optionalManifestConfig(server, dir, &out)
	require.NoError(t, Run(context.Background(), config))
	assert.Empty(t, parseReport(t, &out).Untracked, "the space ended tracked")

	assert.Positive(t, manifestShards(server, mustFind(t, server, "Handbook")), "the mapping is on the landing page")
	guide := mustFind(t, server, "Guide")

	writeFile(t, dir, "a-guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide Renamed -->\n\nA guide.\n")
	out.Reset()
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, guide, mustFind(t, server, "Guide Renamed"), "the second run was tracked")
}

// TestManifestPageOptionalNoOverwriteLeavesExistingPages: --no-overwrite
// promises not to overwrite an edit made in Confluence, and with no manifest
// nothing says what mark last wrote. An existing page is left alone rather
// than overwritten on that doubt; a new one is still created.
func TestManifestPageOptionalNoOverwriteLeavesExistingPages(t *testing.T) {
	server, _ := docsSpace(t)
	existing := server.AddPage("DOCS", "Guide", "page", "")
	server.EditPage(existing.ID, "<p>edited in Confluence</p>")

	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")
	writeFile(t, dir, "intro.md", "<!-- Space: DOCS -->\n<!-- Title: Intro -->\n\nAn intro.\n")

	var out bytes.Buffer
	config := optionalManifestConfig(server, dir, &out)
	config.Tracking.NoOverwrite = true
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "<p>edited in Confluence</p>", server.Page(existing.ID).Body)

	statuses := map[string]report.Status{}
	for _, page := range parseReport(t, &out).Pages {
		statuses[page.Title] = page.Status
	}
	assert.Equal(t, report.StatusSkipped, statuses["Guide"])
	assert.Equal(t, report.StatusPublished, statuses["Intro"])
}

// TestManifestPageOptionalRequiresManifestPage: it says what to do when the
// manifest page is missing, and on its own there is none.
func TestManifestPageOptionalRequiresManifestPage(t *testing.T) {
	server := confluencetest.New(t)
	config := optionalManifestConfig(server, t.TempDir(), io.Discard)
	config.Tracking.ManifestPage = ""

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		"--manifest-page-optional (Tracking.ManifestPageOptional) requires --manifest-page (Tracking.ManifestPage)")
}
