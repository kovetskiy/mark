package mark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noConfluence is a base URL that counts every request made to it, for a run
// that must make none.
func noConfluence(t *testing.T) (string, *atomic.Int64) {
	t.Helper()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "nobody here", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	return server.URL, &requests
}

// compileRun compiles dir's documents and returns what it printed.
func compileRun(t *testing.T, dir, baseURL string, mode ModeConfig, checks []string) (string, error) {
	t.Helper()

	var out strings.Builder
	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Mode:      mode,
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Links:     LinkConfig{CheckLinks: checks},
			Output:    &out,
		},
		Files:      filepath.Join(dir, "a.md"),
		Connection: ConnectionConfig{BaseURL: baseURL, Username: "user", Password: "token"},
	})

	return out.String(), err
}

// TestAnOfflineCompile: with no Confluence to ask, a relative link to another
// document is left as written and still checked against the files, and
// nothing is requested.
func TestAnOfflineCompile(t *testing.T) {
	offline := ModeConfig{CompileOnly: true, Offline: true}

	t.Run("a link to a document is left as written", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nSee [b](./b.md).\n")
		writeFile(t, dir, "b.md", outHeader+"<!-- Title: B -->\n\nB.\n")
		url, requests := noConfluence(t)

		out, err := compileRun(t, dir, url, offline, []string{"internal"})
		require.NoError(t, err)
		assert.Contains(t, out, `href="./b.md"`)
		assert.Zero(t, requests.Load(), "nothing is asked of Confluence")
	})

	t.Run("a link to no file still fails the check", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nSee [b](./nowhere.md) and [c](./c.md).\n")
		writeFile(t, dir, "c.md", "No headers.\n")
		url, requests := noConfluence(t)

		_, err := compileRun(t, dir, url, offline, []string{"internal"})
		require.ErrorContains(t, err, `"./nowhere.md" does not resolve: there is no such file`)
		require.ErrorContains(t, err, `"./c.md" does not resolve: it has no title`)
		assert.Zero(t, requests.Load())
	})

	t.Run("an ac: link cannot be checked", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nSee [b](ac:B).\n")
		url, requests := noConfluence(t)

		_, err := compileRun(t, dir, url, offline, []string{"confluence"})
		require.ErrorContains(t, err, `link "ac:B" cannot be checked: there is no Confluence`)
		assert.Zero(t, requests.Load())
	})

	t.Run("a compile with Confluence still rewrites the link", func(t *testing.T) {
		server := outputServer(t)
		server.AddPage("DOCS", "B", "page", "")
		dir := t.TempDir()
		writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nSee [b](./b.md).\n")
		writeFile(t, dir, "b.md", outHeader+"<!-- Title: B -->\n\nB.\n")

		out, err := compileRun(t, dir, server.URL, ModeConfig{CompileOnly: true}, []string{"internal"})
		require.NoError(t, err)
		assert.Contains(t, out, "/x/")
		assert.NotContains(t, out, `href="./b.md"`)
	})

	t.Run("only a compile can be offline", func(t *testing.T) {
		err := PublishConfig{Mode: ModeConfig{DryRun: true, Offline: true}}.Validate()
		require.ErrorContains(t, err, "Mode.Offline requires Mode.CompileOnly")
	})
}

// TestASourceOnlyLinkIsAWarning: under SourceRootCheckOnly a link passing on a
// file only the source tree holds is left as a path that leads nowhere, and
// the report says so against the document.
func TestASourceOnlyLinkIsAWarning(t *testing.T) {
	staged, source, document := stagedTree(t)

	server, _, _, config := publisherFixture(t, "")
	server.AddPage("DOCS", "Parent", "page", "")
	config.Files = document
	config.Links = LinkConfig{
		CheckLinks: []string{"internal"}, StagedRoot: staged, SourceRoot: source, SourceRootCheckOnly: true,
	}

	var out strings.Builder
	config.Output = &out
	config.Report.OutputFormat = report.FormatJSON
	require.NoError(t, Run(context.Background(), config))

	var got report.Report
	require.NoError(t, json.Unmarshal([]byte(out.String()), &got))
	require.Len(t, got.Pages, 1)

	page := got.Pages[0]
	require.Len(t, page.WarningDetails, 1, "%+v", page)
	warning := page.WarningDetails[0]
	assert.Equal(t, report.WarningSourceOnlyLink, warning.Kind)
	assert.Equal(t, document, warning.File)
	assert.Contains(t, warning.Message, `link "./guide.md" names `+filepath.Join(source, "team", "guide.md"))
	assert.Contains(t, warning.Message, "leads nowhere in Confluence")
	assert.Equal(t, []string{warning.Message}, page.Warnings)
}

// TestReadOnlyRunsCountTheParentsTheyWouldCreate: a parent a real run would
// create for a document, and the page of a document that then fails, both
// exist once a real run has looked them up, so a dry run does not report an
// ac: link to either.
func TestReadOnlyRunsCountTheParentsTheyWouldCreate(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: A -->\n\n"+
		"See [h](ac:Handbook) and [b](ac:B).\n")
	writeFile(t, dir, "b.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Parent: Handbook -->\n"+
		"<!-- Title: B -->\n\nB, broken: [x](./nowhere.md).\n")

	var out strings.Builder
	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Mode:      ModeConfig{DryRun: true},
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Links:     LinkConfig{CheckLinks: []string{"internal", "confluence"}},
			Output:    &out,
		},
		Files:           filepath.Join(dir, "*.md"),
		ContinueOnError: true,
		Connection:      ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
	})
	// An ac: link left unresolved would be the run's error in place of
	// the files that failed.
	var failed *FilesFailedError
	require.ErrorAs(t, err, &failed, "b.md's own link is broken, and nothing else: %v", err)
	require.Len(t, failed.Errors, 1)
	assert.ErrorContains(t, failed.Errors[0], "./nowhere.md")
}
