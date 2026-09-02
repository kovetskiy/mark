package mark

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoOverwriteDoesNotPrintTheURLOfAPageItLeftAlone: the drift branch returns
// the page, and the caller took a page coming back as proof one had been
// written -- so a run said "leaving it alone" and then, on the very next line,
// "page successfully updated", and printed the URL among the pages it had
// published.
func TestNoOverwriteDoesNotPrintTheURLOfAPageItLeftAlone(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	// Somebody edits the page in Confluence, which is what --no-overwrite is
	// there to notice.
	server.EditPage(id, "<p>By hand.</p>")

	var out bytes.Buffer
	config.OutputFormat = report.FormatURL
	config.Output = &out

	require.NoError(t, Run(config))

	assert.Empty(t, strings.TrimSpace(out.String()),
		"a page this run deliberately did not write is not one of the pages it wrote")
}

// TestNoOverwriteStillReportsTheSkip: silence in the URL list is not silence in
// the report, which is where the reason belongs.
func TestNoOverwriteStillReportsTheSkip(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>By hand.</p>")

	var out bytes.Buffer
	config.OutputFormat = report.FormatJSON
	config.Output = &out

	require.NoError(t, Run(config))

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Len(t, parsed.Pages, 1)

	assert.Equal(t, report.StatusSkipped, parsed.Pages[0].Status)
	assert.Contains(t, parsed.Pages[0].Reason, "edited in Confluence")
}

// TestReportIsWrittenWhenTheRunFails: the report used to be written only after
// every check had passed, so --output-format json produced nothing at all when
// a run published its pages and then failed on an unresolved link -- which is
// exactly when an account of what was published is worth having.
func TestReportIsWrittenWhenTheRunFails(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nSee [it](ac:Nowhere).\n")

	var out bytes.Buffer

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:        filepath.Join(dir, "doc.md"),
		Features:     []string{"mention"},
		CheckLinks:   []string{"confluence"},
		OutputFormat: report.FormatJSON,
		Output:       &out,
	})

	require.Error(t, err, "the unresolved link still fails the run")

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed), "and the report is still written")
	require.Len(t, parsed.Pages, 1)
	assert.Equal(t, report.StatusPublished, parsed.Pages[0].Status,
		"the page did publish, whatever became of the link")
}

// TestUnresolvedLinkNamesTheDocument: the resolver was given the directory the
// document sits in as the source of its messages, so with several documents in
// one folder the message named the folder and left the reader to work out which
// file it meant.
func TestUnresolvedLinkNamesTheDocument(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "innocent.md", header+"<!-- Title: Innocent -->\n\nNothing wrong here.\n")
	writeFile(t, dir, "guilty.md", header+"<!-- Title: Guilty -->\n\nSee [it](ac:Nowhere).\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:      filepath.Join(dir, "*.md"),
		Features:   []string{"mention"},
		CheckLinks: []string{"confluence"},
		Output:     io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "guilty.md", "the message names the file to open")
	assert.NotContains(t, err.Error(), "innocent.md")
}
