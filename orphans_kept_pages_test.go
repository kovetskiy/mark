package mark

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keptOrphan returns the report's entry for a page --on-orphan left alone.
func keptOrphan(t *testing.T, got *report.Report, pageID string) report.Orphan {
	t.Helper()

	for _, orphan := range got.Orphans {
		if orphan.PageID == pageID {
			return orphan
		}
	}
	require.Failf(t, "no orphan reported", "page %s is not among %+v", pageID, got.Orphans)

	return report.Orphan{}
}

// TestOnOrphanLeavesTheSpaceHomepage: a document titled like the homepage
// publishes to it, and removing the document must not remove the page the
// whole space hangs from, which `mark page delete` and `archive` refuse too.
func TestOnOrphanLeavesTheSpaceHomepage(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive} {
		for _, platform := range []string{"cloud", "data center"} {
			t.Run(string(action)+"/"+platform, func(t *testing.T) {
				server := confluencetest.New(t)
				if platform == "data center" {
					server.SetDataCenter()
				}
				home := server.AddPage("DOCS", "Home", "page", "")
				server.SetHomepage("DOCS", home.ID)
				// Beside the homepage, so that the homepage has no children.
				server.AddPage("DOCS", "Parent", "page", "")

				dir := t.TempDir()
				writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
				writeFile(t, dir, "gone.md", markdownWithTitle("Gone"))
				homeFile := writeFile(t, dir, "home.md", parentDoc("Home"))

				config := trackingConfig(server, filepath.Join(dir, "*.md"))
				config.Tracking.OnOrphan = action
				require.NoError(t, Run(context.Background(), config))
				require.Equal(t, home.ID, pageIDTitled(t, server, "Home"), "the document publishes to the homepage")
				goneID := pageIDTitled(t, server, "Gone")

				require.NoError(t, os.Remove(homeFile))
				require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

				logged := captureLog(t, func() {
					previewOf(t, config, report.FormatJSON)
				})
				assert.Contains(t, logged, `page \"Home\"`)
				assert.Contains(t, logged, `is left alone`)
				assert.NotContains(t, logged, `page \"Home\" would be`)

				wantReason := "it is the homepage of space DOCS, and mark does not remove it"
				if platform == "data center" {
					wantReason = `the page manifest of space "DOCS" is kept on it`
				}

				for range 2 {
					got := decodeReport(t, []byte(publishJSON(t, config)))

					page := server.Page(home.ID)
					require.NotNil(t, page)
					assert.False(t, page.Trashed, "the homepage stays")
					assert.False(t, page.Archived, "the homepage stays")

					kept := keptOrphan(t, got, home.ID)
					assert.Equal(t, homeFile, kept.File)
					assert.Equal(t, report.OnOrphanReport, kept.Action)
					assert.Equal(t, wantReason, kept.Reason)
				}

				gone := server.Page(goneID)
				require.NotNil(t, gone)
				if action == report.OnOrphanDelete {
					assert.True(t, gone.Trashed, "any other orphan is still removed")
				} else {
					assert.True(t, gone.Archived, "any other orphan is still removed")
				}
			})
		}
	}
}

// TestOnOrphanLeavesTheManifestPage: the page --manifest-page names holds the
// record every tracked run reads, so a document that published to it and is
// then removed leaves the page where it is, and later runs keep working.
func TestOnOrphanLeavesTheManifestPage(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive} {
		for _, platform := range []string{"cloud", "data center"} {
			t.Run(string(action)+"/"+platform, func(t *testing.T) {
				server := confluencetest.New(t)
				if platform == "data center" {
					server.SetDataCenter()
				}
				home := server.AddPage("DOCS", "Home", "page", "")
				server.SetHomepage("DOCS", home.ID)
				parent := server.AddPage("DOCS", "Parent", "page", home.ID)
				records := server.AddPage("DOCS", "Records", "page", parent.ID)

				dir := t.TempDir()
				writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
				writeFile(t, dir, "gone.md", markdownWithTitle("Gone"))
				recordsFile := writeFile(t, dir, "records.md", markdownWithTitle("Records"))

				config := trackingConfig(server, filepath.Join(dir, "*.md"))
				config.Tracking.OnOrphan = action
				config.Tracking.ManifestPage = "Records"
				require.NoError(t, Run(context.Background(), config))
				require.Equal(t, records.ID, pageIDTitled(t, server, "Records"), "the document publishes to the manifest page")
				goneID := pageIDTitled(t, server, "Gone")

				require.NoError(t, os.Remove(recordsFile))
				require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

				logged := captureLog(t, func() {
					previewOf(t, config, report.FormatJSON)
				})
				assert.Contains(t, logged, `page \"Records\" (`+records.ID+`) is left alone`)
				assert.NotContains(t, logged, `page \"Records\" would be`)

				// The second run is the one that needs the manifest page still there.
				for range 2 {
					got := decodeReport(t, []byte(publishJSON(t, config)))

					page := server.Page(records.ID)
					require.NotNil(t, page)
					assert.False(t, page.Trashed, "the manifest page stays")
					assert.False(t, page.Archived, "the manifest page stays")

					kept := keptOrphan(t, got, records.ID)
					assert.Equal(t, recordsFile, kept.File)
					assert.Equal(t, report.OnOrphanReport, kept.Action)
					assert.Equal(t, `the page manifest of space "DOCS" is kept on it`, kept.Reason)
				}

				gone := server.Page(goneID)
				require.NotNil(t, gone)
				if action == report.OnOrphanDelete {
					assert.True(t, gone.Trashed, "any other orphan is still removed")
				} else {
					assert.True(t, gone.Archived, "any other orphan is still removed")
				}
			})
		}
	}
}

// TestOnOrphanReportForgetsTheManifestPage: only removing is held back. A
// reporting run names the page and stops tracking it like any other.
func TestOnOrphanReportForgetsTheManifestPage(t *testing.T) {
	server, _ := docsSpace(t)
	records := server.AddPage("DOCS", "Records", "page", pageIDTitled(t, server, "Parent"))

	dir := t.TempDir()
	writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
	recordsFile := writeFile(t, dir, "records.md", markdownWithTitle("Records"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = report.OnOrphanReport
	config.Tracking.ManifestPage = "Records"
	require.NoError(t, Run(context.Background(), config))
	require.NoError(t, os.Remove(recordsFile))

	got := decodeReport(t, []byte(publishJSON(t, config)))
	kept := keptOrphan(t, got, records.ID)
	assert.Empty(t, kept.Reason)

	got = decodeReport(t, []byte(publishJSON(t, config)))
	assert.Empty(t, got.Orphans, "reported once, then no longer tracked")
	assert.False(t, server.Page(records.ID).Trashed)
}
