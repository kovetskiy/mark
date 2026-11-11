package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adrText is one of the ADRs internal/similar measures its scores on:
// adr-0001 renamed and edited is about 0.78 alike to it, rewritten about
// 0.37, and adr-0002 and adr-0003 under 0.05.
func adrText(t *testing.T, name string) string {
	t.Helper()

	text, err := os.ReadFile("internal/similar/testdata/" + name + ".md")
	require.NoError(t, err)

	return string(text)
}

// adrDocument is a document of key titled title in space, holding body.
func adrDocument(key, space, title, body string) Document {
	return Document{
		Path: key,
		Content: []byte("<!-- Space: " + space + " -->\n<!-- Parent: Parent -->\n<!-- Title: " + title + " -->\n\n" +
			body),
		FS: fstest.MapFS{},
	}
}

// similarFixture publishes three ADRs under the adr scope and returns the
// server, that run and the page of the first, use-postgres.md.
func similarFixture(t *testing.T) (*confluencetest.Server, Config, string) {
	t.Helper()

	server, _ := docsSpace(t)
	full := documentsConfig(server,
		adrDocument("use-postgres.md", "DOCS", "Use PostgreSQL", adrText(t, "adr-0001")),
		adrDocument("events.md", "DOCS", "Publish events", adrText(t, "adr-0002")),
		adrDocument("migrations.md", "DOCS", "Run migrations", adrText(t, "adr-0003")),
	)
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))

	return server, full, mustFind(t, server, "Use PostgreSQL")
}

// storeOrders is use-postgres.md renamed, retitled and edited.
func storeOrders(t *testing.T) Document {
	t.Helper()

	return adrDocument("store-orders.md", "DOCS", "Store orders in PostgreSQL", adrText(t, "adr-0001-edited"))
}

// similarly is config following renames from threshold on, writing its JSON
// report to out.
func similarly(config Config, threshold float64, out *bytes.Buffer) Config {
	config.Tracking.RenameSimilarity = threshold
	config.Report.OutputFormat = report.FormatJSON
	config.Output = out

	return config
}

// renameWarnings is the rename warnings the JSON report gives the document at
// file.
func renameWarnings(t *testing.T, out *bytes.Buffer, file string) []string {
	t.Helper()

	var run report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &run), out.String())

	var messages []string
	for _, pg := range run.Pages {
		if pg.File != file {
			continue
		}
		for _, warning := range pg.WarningDetails {
			if warning.Kind == report.WarningRename {
				messages = append(messages, warning.Message)
			}
		}
	}

	return messages
}

// assertFollowed checks that key publishes to pageID, retitled, and that the
// old key handed its entry over.
func assertFollowed(t *testing.T, server *confluencetest.Server, key, old, pageID, title string) {
	t.Helper()

	entry, ok := lookupKey(t, server, key)
	require.True(t, ok)
	assert.Equal(t, pageID, entry.PageID, "the gone key's page follows the document")
	assert.Equal(t, adrScope, entry.Glob)
	assert.Equal(t, title, server.Page(pageID).Title)
	assert.False(t, server.Page(pageID).Trashed)
	_, ok = lookupKey(t, server, old)
	assert.False(t, ok, "the old key handed its entry over")
}

// assertNotFollowed checks that key has a page of its own, not pageID.
func assertNotFollowed(t *testing.T, server *confluencetest.Server, key, pageID string) {
	t.Helper()

	entry, ok := lookupKey(t, server, key)
	require.True(t, ok)
	assert.NotEqual(t, pageID, entry.PageID, "a new page")
}

// TestSimilarRenameUnderPartial: a document renamed, retitled and edited in
// one change, in a partial run given the scope's other keys, is followed to
// its page with RenameSimilarity, and the report names both keys and the
// score. Without it the document gets a new page and the old one is deleted
// as an orphan.
func TestSimilarRenameUnderPartial(t *testing.T) {
	server, full, oldID := similarFixture(t)
	keys := []string{"events.md", "migrations.md"}

	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), similarly(partialDocumentsOf(full, keys, storeOrders(t)), 0.6, &out)))

	assertFollowed(t, server, "store-orders.md", "use-postgres.md", oldID, "Store orders in PostgreSQL")
	assert.Contains(t, server.Page(oldID).Body, "Backups are taken by the operator")
	warnings := renameWarnings(t, &out, "store-orders.md")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "store-orders.md is taken for use-postgres.md renamed")
	assert.Contains(t, warnings[0], "page "+oldID)
	assert.Contains(t, warnings[0], "0.78 alike")

	off, offFull, offOldID := similarFixture(t)
	require.NoError(t, Run(context.Background(), partialDocumentsOf(offFull, keys, storeOrders(t))))
	assertNotFollowed(t, off, "store-orders.md", offOldID)
	assert.True(t, off.Page(offOldID).Trashed, "an orphan, deleted")
}

// TestSimilarRenameInAFullScopedRun: the same in a run publishing the whole
// scope.
func TestSimilarRenameInAFullScopedRun(t *testing.T) {
	renamed := func(full Config) Config {
		full.Documents = []Document{storeOrders(t), full.Documents[1], full.Documents[2]}
		full.Tracking.OnOrphan = report.OnOrphanDelete

		return full
	}

	server, full, oldID := similarFixture(t)
	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), similarly(renamed(full), 0.6, &out)))
	assertFollowed(t, server, "store-orders.md", "use-postgres.md", oldID, "Store orders in PostgreSQL")

	off, offFull, offOldID := similarFixture(t)
	require.NoError(t, Run(context.Background(), renamed(offFull)))
	assertNotFollowed(t, off, "store-orders.md", offOldID)
	assert.True(t, off.Page(offOldID).Trashed)
}

// TestSimilarRenameBelowTheThreshold: a document rewritten more than renamed
// is not followed, and nothing is warned.
func TestSimilarRenameBelowTheThreshold(t *testing.T) {
	server, full, oldID := similarFixture(t)

	rewritten := adrDocument("store-orders.md", "DOCS", "Store orders in PostgreSQL", adrText(t, "adr-0001-rewritten"))
	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(full, []string{"events.md", "migrations.md"}, rewritten), 0.6, &out)))

	assertNotFollowed(t, server, "store-orders.md", oldID)
	assert.Empty(t, renameWarnings(t, &out, "store-orders.md"))
}

// TestSimilarRenameAmbiguous: two gone keys about as alike to the document
// follow neither, and say so.
func TestSimilarRenameAmbiguous(t *testing.T) {
	server, _ := docsSpace(t)
	original := adrText(t, "adr-0001")
	full := documentsConfig(server,
		adrDocument("use-postgres.md", "DOCS", "Use PostgreSQL", original),
		adrDocument("use-postgres-copy.md", "DOCS", "Use PostgreSQL (copy)", original+"\nA copy.\n"),
		adrDocument("events.md", "DOCS", "Publish events", adrText(t, "adr-0002")),
	)
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))
	oldID, copyID := mustFind(t, server, "Use PostgreSQL"), mustFind(t, server, "Use PostgreSQL (copy)")

	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(full, []string{"events.md"}, storeOrders(t)), 0.6, &out)))

	entry, ok := lookupKey(t, server, "store-orders.md")
	require.True(t, ok)
	assert.NotContains(t, []string{oldID, copyID}, entry.PageID)
	warnings := renameWarnings(t, &out, "store-orders.md")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "store-orders.md is not taken for a rename")
	assert.Contains(t, warnings[0], "use-postgres.md (0.78)")
	assert.Contains(t, warnings[0], "use-postgres-copy.md (0.7")
}

// TestSimilarRenameIsOneToOne: of two documents alike to one gone key, only
// the clearly closer is taken for it; the other gets a page of its own.
func TestSimilarRenameIsOneToOne(t *testing.T) {
	server, full, oldID := similarFixture(t)

	rewritten := adrDocument("managed-postgres.md", "DOCS", "Managed PostgreSQL", adrText(t, "adr-0001-rewritten"))
	var out bytes.Buffer
	// Low enough for the rewrite, about 0.37, to be a candidate too.
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(full, []string{"events.md", "migrations.md"}, rewritten, storeOrders(t)), 0.3, &out)))

	assertFollowed(t, server, "store-orders.md", "use-postgres.md", oldID, "Store orders in PostgreSQL")
	assertNotFollowed(t, server, "managed-postgres.md", oldID)
	assert.NotEmpty(t, renameWarnings(t, &out, "managed-postgres.md"))

	// Two about as close: neither.
	again, againFull, againID := similarFixture(t)
	twin := storeOrders(t)
	twin.Path = "store-orders-twin.md"
	twin.Content = append(bytes.ReplaceAll(twin.Content, []byte("Store orders in"), []byte("Keep orders in")),
		"\nOne more line.\n"...)
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(againFull, []string{"events.md", "migrations.md"}, storeOrders(t), twin), 0.6, &out)))
	assertNotFollowed(t, again, "store-orders.md", againID)
	assertNotFollowed(t, again, "store-orders-twin.md", againID)
}

// TestExactRenameWinsOverSimilarity: a document with a gone key's exact
// content takes it, and a document only alike to it, coming first, does not
// take it from under it.
func TestExactRenameWinsOverSimilarity(t *testing.T) {
	server, full, oldID := similarFixture(t)

	exact := adrDocument("postgres.md", "DOCS", "Use PostgreSQL", adrText(t, "adr-0001"))
	exact.Content = full.Documents[0].Content
	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(full, []string{"events.md", "migrations.md"}, storeOrders(t), exact), 0.6, &out)))

	entry, ok := lookupKey(t, server, "postgres.md")
	require.True(t, ok)
	assert.Equal(t, oldID, entry.PageID)
	assertNotFollowed(t, server, "store-orders.md", oldID)
	assert.Empty(t, renameWarnings(t, &out, "store-orders.md"))
}

// TestMoveByTitleWinsOverSimilarity: a document finding a gone key's page by
// its title takes it, and a document only alike to the gone key, coming
// first, does not take it from under it.
func TestMoveByTitleWinsOverSimilarity(t *testing.T) {
	server, full, oldID := similarFixture(t)

	moved := adrDocument("postgres.md", "DOCS", "Use PostgreSQL", adrText(t, "adr-0001-rewritten"))
	require.NoError(t, Run(context.Background(), similarly(
		partialDocumentsOf(full, []string{"events.md", "migrations.md"}, storeOrders(t), moved), 0.6, &bytes.Buffer{})))

	entry, ok := lookupKey(t, server, "postgres.md")
	require.True(t, ok)
	assert.Equal(t, oldID, entry.PageID)
	assertNotFollowed(t, server, "store-orders.md", oldID)
}

// TestSimilarRenameLeavesADriftedPage: under NoOverwrite a gone key whose
// page was edited in Confluence is never taken by similarity; one not edited
// is.
func TestSimilarRenameLeavesADriftedPage(t *testing.T) {
	keys := []string{"events.md", "migrations.md"}

	server, full, oldID := similarFixture(t)
	full.Tracking.NoOverwrite = true
	require.NoError(t, Run(context.Background(), similarly(partialDocumentsOf(full, keys, storeOrders(t)), 0.6, &bytes.Buffer{})))
	assertFollowed(t, server, "store-orders.md", "use-postgres.md", oldID, "Store orders in PostgreSQL")

	drifted, driftedFull, driftedID := similarFixture(t)
	drifted.EditPage(driftedID, drifted.Page(driftedID).Body)
	driftedFull.Tracking.NoOverwrite = true
	partial := partialDocumentsOf(driftedFull, keys, storeOrders(t))
	partial.Tracking.OnOrphan = report.OnOrphanReport
	logged := captureLog(t, func() {
		require.NoError(t, Run(context.Background(), similarly(partial, 0.6, &bytes.Buffer{})))
	})
	assertNotFollowed(t, drifted, "store-orders.md", driftedID)
	assert.Equal(t, "Use PostgreSQL", drifted.Page(driftedID).Title)
	assert.Contains(t, logged, "use-postgres.md is not compared for a rename")
}

// TestSimilarRenameStaysInScopeAndSpace: a gone key of another scope, or a
// document publishing to another space, is never matched.
func TestSimilarRenameStaysInScopeAndSpace(t *testing.T) {
	server, full, oldID := similarFixture(t)

	otherScope := full
	otherScope.DocumentScope = "other"
	otherScope.Documents = []Document{storeOrders(t)}
	require.NoError(t, Run(context.Background(), similarly(otherScope, 0.6, &bytes.Buffer{})))
	assertNotFollowed(t, server, "store-orders.md", oldID)
	assert.Equal(t, "Use PostgreSQL", server.Page(oldID).Title)

	spaces, spacesFull, spacesID := similarFixture(t)
	home := spaces.AddPage("OTHER", "Other home", "page", "")
	spaces.SetHomepage("OTHER", home.ID)
	spaces.AddPage("OTHER", "Parent", "page", home.ID)
	elsewhere := adrDocument("store-orders.md", "OTHER", "Store orders in PostgreSQL", adrText(t, "adr-0001-edited"))
	partial := partialDocumentsOf(spacesFull, []string{"events.md", "migrations.md"}, elsewhere)
	partial.Tracking.OnOrphan = report.OnOrphanReport
	require.NoError(t, Run(context.Background(), similarly(partial, 0.6, &bytes.Buffer{})))
	assert.Equal(t, "Use PostgreSQL", spaces.Page(spacesID).Title)
	_, ok := lookupKey(t, spaces, "use-postgres.md")
	assert.True(t, ok, "still the gone key's, in its own space")
}

// TestSimilarRenameCostsNothingWithNothingToMatch: a run with no document
// lacking a page, or no gone key, makes the same requests with the setting as
// without it.
func TestSimilarRenameCostsNothingWithNothingToMatch(t *testing.T) {
	runs := map[string]func(t *testing.T, full Config) Config{
		"the same documents": func(_ *testing.T, full Config) Config {
			return full
		},
		"a key gone": func(_ *testing.T, full Config) Config {
			full.Documents = full.Documents[1:]
			return full
		},
		"a document new": func(t *testing.T, full Config) Config {
			full.Documents = append(full.Documents, adrDocument("new.md", "DOCS", "New", adrText(t, "adr-0001-rewritten")))
			return full
		},
		"partial, a key edited": func(t *testing.T, full Config) Config {
			edited := full.Documents[0]
			edited.Content = append(edited.Content, "Edited.\n"...)
			return partialDocumentsOf(full, []string{"events.md", "migrations.md"}, edited)
		},
	}

	for name, run := range runs {
		t.Run(name, func(t *testing.T) {
			requests := map[float64][]confluencetest.Request{}
			for _, threshold := range []float64{0, 0.6} {
				server, full, _ := similarFixture(t)
				server.ResetRequests()
				config := run(t, full)
				config.Tracking.RenameSimilarity = threshold
				require.NoError(t, Run(context.Background(), config))
				requests[threshold] = server.Requests()
			}

			assert.Equal(t, requests[0], requests[0.6])
		})
	}

	t.Run("a rename to match", func(t *testing.T) {
		counts := map[float64]int{}
		for _, threshold := range []float64{0, 0.6} {
			server, full, _ := similarFixture(t)
			server.ResetRequests()
			config := partialDocumentsOf(full, []string{"events.md", "migrations.md"}, storeOrders(t))
			config.Tracking.RenameSimilarity = threshold
			require.NoError(t, Run(context.Background(), config))
			for _, request := range server.Requests() {
				if request.Method == http.MethodGet && strings.Contains(request.Query, "body.storage") {
					counts[threshold]++
				}
			}
		}

		assert.Equal(t, counts[0]+1, counts[0.6], "the gone page is read, once")
	})
}

// TestSimilarRenameDryRun: a dry run says the document would follow the gone
// key's page, warns as the real run does, and changes nothing.
func TestSimilarRenameDryRun(t *testing.T) {
	server, full, oldID := similarFixture(t)
	before, ok := lookupKey(t, server, "use-postgres.md")
	require.True(t, ok)

	partial := partialDocumentsOf(full, []string{"events.md", "migrations.md"}, storeOrders(t))
	partial.Tracking.RenameSimilarity = 0.6
	logged := captureLog(t, func() { previewOf(t, partial, report.FormatJSON) })

	assert.Contains(t, logged, "store-orders.md would be treated as a rename of an already published document, updating page "+oldID)
	assert.Contains(t, logged, "store-orders.md is taken for use-postgres.md renamed")
	assert.False(t, strings.Contains(logged, "had no matching source file"), "the followed key is no orphan")
	assert.Equal(t, "Use PostgreSQL", server.Page(oldID).Title)
	after, ok := lookupKey(t, server, "use-postgres.md")
	require.True(t, ok)
	assert.Equal(t, before, after)
	_, ok = lookupKey(t, server, "store-orders.md")
	assert.False(t, ok)
}

// TestRenameSimilarityIsChecked: a value that is no similarity, or one
// without tracking, is refused before anything is published.
func TestRenameSimilarityIsChecked(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, adr("a"))
	config.DocumentScope = "adr"

	for _, value := range []float64{-0.1, 1.5} {
		bad := config
		bad.Tracking.RenameSimilarity = value
		require.ErrorContains(t, Run(context.Background(), bad), "is not a similarity")
	}

	untracked := config
	untracked.DocumentScope = ""
	untracked.Tracking.TrackPages = false
	untracked.Tracking.RenameSimilarity = 0.6
	require.ErrorContains(t, Run(context.Background(), untracked), "requires Tracking.TrackPages")
	assert.Empty(t, pageIDTitled(t, server, "Record a"))
}

// TestSimilarRenameNotForTemplateText: a record deleted and an unrelated one
// added in one change, both mostly a template's text, are as alike to each
// other as to every record that stays, so the new one is not taken for the
// deleted one.
func TestSimilarRenameNotForTemplateText(t *testing.T) {
	template := "## Status\n\nProposed\n\n## Context\n\nDescribe the forces at play, including technological, " +
		"political, social and project local ones, and why a decision is needed now.\n\n## Decision\n\n" +
		"Describe the response to these forces, stated in full sentences, with active voice.\n\n" +
		"## Consequences\n\nDescribe the resulting context, after applying the decision, good and bad.\n\n"
	record := func(key, title, topic string) Document {
		return adrDocument(key, "DOCS", title, template+topic+"\n")
	}

	server, _ := docsSpace(t)
	full := documentsConfig(server,
		record("cache.md", "Cache sessions", "Sessions go to the cache."),
		record("queue.md", "Queue jobs", "Jobs go to the queue."),
		record("logs.md", "Ship logs", "Logs go to the collector."),
	)
	full.DocumentScope = "adr"
	require.NoError(t, Run(context.Background(), full))
	cacheID := mustFind(t, server, "Cache sessions")

	var out bytes.Buffer
	unrelated := record("tracing.md", "Trace requests", "Requests carry a trace id.")
	partial := partialDocumentsOf(full, []string{"logs.md"}, full.Documents[1], unrelated)
	partial.Tracking.OnOrphan = report.OnOrphanReport
	require.NoError(t, Run(context.Background(), similarly(partial, 0.6, &out)))

	assertNotFollowed(t, server, "tracing.md", cacheID)
	assert.Equal(t, "Cache sessions", server.Page(cacheID).Title)
	warnings := renameWarnings(t, &out, "tracing.md")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "cache.md (0.")
}
