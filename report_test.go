package mark

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outputServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

func runWithFormat(t *testing.T, format string, files map[string]string) (string, error) {
	t.Helper()

	server := outputServer(t)
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}

	var out strings.Builder
	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		OutputFormat: format, ContinueOnError: true, Output: &out,
	})

	return out.String(), err
}

const outHeader = "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

func TestOutputFormatURLIsUnchanged(t *testing.T) {
	out, err := runWithFormat(t, "", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "/display/DOCS/")
	assert.NotContains(t, out, "{", "the default output is not JSON")
}

func TestOutputFormatJSON(t *testing.T) {
	out, err := runWithFormat(t, "json", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
		"b.md": outHeader + "<!-- Title: B -->\n<!-- Synchronized: false -->\n\nB.\n",
	})
	require.NoError(t, err)

	var got struct {
		Pages []struct {
			File, Status, Space, Title, PageID, URL, Reason string
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), "the output must parse as JSON")
	require.Len(t, got.Pages, 2)

	byStatus := map[string]string{}
	for _, page := range got.Pages {
		byStatus[page.Status] = page.File
		assert.NotEmpty(t, page.File)
	}

	require.Contains(t, byStatus, "published")
	require.Contains(t, byStatus, "skipped")
	assert.True(t, strings.HasSuffix(byStatus["published"], "a.md"))
	assert.True(t, strings.HasSuffix(byStatus["skipped"], "b.md"))

	// The published page carries what a script would want to act on.
	for _, page := range got.Pages {
		if page.Status == "published" {
			assert.Equal(t, "DOCS", page.Space)
			assert.Equal(t, "A", page.Title)
			assert.NotEmpty(t, page.PageID)
			assert.Contains(t, page.URL, "/display/DOCS/")
		}
	}
}

// TestOutputFormatGitHubAnnotatesAFailingFile is why the format exists: the
// failure has to name the file so it lands on it in a pull request.
func TestOutputFormatGitHubAnnotatesAFailingFile(t *testing.T) {
	out, err := runWithFormat(t, "github", map[string]string{
		"good.md": outHeader + "<!-- Title: Good -->\n\nFine.\n",
		"bad.md":  outHeader + "<!-- Title: Bad -->\n\n<!-- ac:ignore -->\nunclosed\n",
	})
	require.Error(t, err, "the unclosed region should fail its file")

	var errorLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "::error") {
			errorLine = line
		}
	}

	require.NotEmpty(t, errorLine, "a failing file must produce an error annotation")
	assert.Contains(t, errorLine, "file=")
	assert.Contains(t, errorLine, "bad.md")
	assert.Contains(t, out, "::notice", "the page that published should be noticed")
	assert.NotContains(t, out, "\n\n::", "annotations are one per line")
}

func TestOutputFormatRejectsAnUnknownValue(t *testing.T) {
	_, err := runWithFormat(t, "yaml", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output-format")
}

// TestOutputFormatGitHubReportsOrphanActions.
func TestOutputFormatGitHubReportsOrphanActions(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "keep.md", outHeader+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", outHeader+"<!-- Title: Gone -->\n\nGone.\n")

	var first strings.Builder
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", OutputFormat: "github", Output: &first,
	}
	require.NoError(t, Run(config))

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

	var second strings.Builder
	config.Output = &second
	require.NoError(t, Run(config))

	assert.Contains(t, second.String(), "::warning")
	assert.Contains(t, second.String(), "gone.md")
}

// orphanReport publishes keep.md and gone.md, removes gone.md, and returns what
// the second run wrote in format together with the id of Gone's page.
func orphanReport(t *testing.T, onOrphan, format string) (string, string) {
	t.Helper()

	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "keep.md", outHeader+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", outHeader+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: onOrphan, OutputFormat: format, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

	var out strings.Builder
	config.Output = &out
	require.NoError(t, Run(config))

	return out.String(), gone.ID
}

// TestOutputFormatJSONNamesOrphans: an orphan was recorded with its file and
// action only, so the report said a page had been deleted without saying which,
// and a run that only reported orphans -- the default -- recorded none at all.
func TestOutputFormatJSONNamesOrphans(t *testing.T) {
	for _, action := range []string{page.OnOrphanReport, page.OnOrphanDelete, page.OnOrphanArchive} {
		t.Run(action, func(t *testing.T) {
			out, id := orphanReport(t, action, report.FormatJSON)

			var parsed report.Report
			require.NoError(t, json.Unmarshal([]byte(out), &parsed))
			require.Len(t, parsed.Orphans, 1)

			orphan := parsed.Orphans[0]
			assert.Equal(t, "gone.md", filepath.Base(orphan.File))
			assert.Equal(t, id, orphan.PageID)
			assert.Equal(t, "Gone", orphan.Title)
			assert.Equal(t, action, orphan.Action)
		})
	}
}

// TestOutputFormatGitHubNamesOrphans: the annotation read `page "" was
// deleted`, and the line meant for an orphan that was only reported could not
// be reached.
func TestOutputFormatGitHubNamesOrphans(t *testing.T) {
	cases := map[string]string{
		page.OnOrphanReport:  `page "Gone" has no source file`,
		page.OnOrphanDelete:  `page "Gone" was deleted: its source file is gone`,
		page.OnOrphanArchive: `page "Gone" was archived: its source file is gone`,
	}

	for action, want := range cases {
		t.Run(action, func(t *testing.T) {
			out, _ := orphanReport(t, action, report.FormatGitHub)

			var line string
			for _, candidate := range strings.Split(out, "\n") {
				if strings.Contains(candidate, "gone.md") {
					line = candidate
				}
			}

			require.NotEmpty(t, line, "the orphan must be annotated against its file")
			assert.True(t, strings.HasPrefix(line, "::warning file="), line)
			assert.True(t, strings.HasSuffix(line, "::"+want), line)
		})
	}
}

// reportedPages runs mark with --output-format json and returns each document's
// entries in the report, by file name.
func reportedPages(t *testing.T, config Config) (map[string][]report.Page, error) {
	t.Helper()

	var out strings.Builder
	config.OutputFormat = report.FormatJSON
	config.Output = &out
	runErr := Run(config)

	var parsed report.Report
	require.NoError(t, json.Unmarshal([]byte(out.String()), &parsed), "the output must parse as JSON")

	byFile := map[string][]report.Page{}
	for _, page := range parsed.Pages {
		name := filepath.Base(page.File)
		byFile[name] = append(byFile[name], page)
	}

	return byFile, runErr
}

// failUpdatesCarrying makes the fake refuse a page update whose body fails
// says so, reading the body without taking it from the handler.
func failUpdatesCarrying(server *confluencetest.Server, fails func(body string) bool) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/content/") {
			return 0, "", false
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			return 0, "", false
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		if !fails(string(body)) {
			return 0, "", false
		}

		return http.StatusBadRequest, `{"message":"refused"}`, true
	})
}

// TestReportDoesNotCallAFailedUpdatePublished: a document was recorded as
// published before its body was even checked, let alone sent, so the report's
// word for it depended on something else recording the failure over it.
func TestReportDoesNotCallAFailedUpdatePublished(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "good.md", outHeader+"<!-- Title: Good -->\n\nFine.\n")
	writeFile(t, dir, "bad.md", outHeader+"<!-- Title: Bad -->\n\nRefused.\n")

	failUpdatesCarrying(server, func(body string) bool {
		return strings.Contains(body, "Refused.")
	})

	byFile, err := reportedPages(t, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		ContinueOnError: true,
	})
	require.Error(t, err)

	require.Len(t, byFile["bad.md"], 1, "one entry per document")
	assert.Equal(t, report.StatusFailed, byFile["bad.md"][0].Status)
	assert.Contains(t, byFile["bad.md"][0].Reason, "unable to update page")

	require.Len(t, byFile["good.md"], 1)
	assert.Equal(t, report.StatusPublished, byFile["good.md"][0].Status)
}

// TestReportRecordsAFailureInTheSecondPass: a document published again once the
// pages it links to exist was reported as published even when that second
// publish failed, since only the first pass recorded failures.
func TestReportRecordsAFailureInTheSecondPass(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "a-doc.md", outHeader+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", outHeader+"<!-- Title: B -->\n\nB.\n")

	// A is written once in the first pass and again in the second; only the
	// second is refused.
	updatesOfA := 0
	failUpdatesCarrying(server, func(body string) bool {
		if !strings.Contains(body, `"title":"A"`) {
			return false
		}
		updatesOfA++

		return updatesOfA > 1
	})

	byFile, err := reportedPages(t, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
	})
	require.Error(t, err)
	require.Equal(t, 2, updatesOfA, "the document should have been published twice")

	require.Len(t, byFile["a-doc.md"], 1, "one entry per document, not published and failed")
	assert.Equal(t, report.StatusFailed, byFile["a-doc.md"][0].Status)
	assert.Contains(t, byFile["a-doc.md"][0].Reason, "unable to update page")

	require.Len(t, byFile["b-doc.md"], 1)
	assert.Equal(t, report.StatusPublished, byFile["b-doc.md"][0].Status)
}

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

// failingWriter refuses every write, as a closed pipe or a full disk would.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestReportWriteFailureFailsTheRun: the deferred write assigned its error to a
// local the function never returned, so a run whose report could not be
// written exited zero, and whatever was waiting on the JSON got nothing.
func TestReportWriteFailureFailsTheRun(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nFine.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:        filepath.Join(dir, "doc.md"),
		Features:     []string{"mention"},
		OutputFormat: report.FormatJSON,
		Output:       failingWriter{},
	})

	require.Error(t, err, "a report that could not be written fails the run")
	assert.Contains(t, err.Error(), "unable to write the run report")
	assert.Contains(t, err.Error(), "disk full")
}

// TestReportWriteFailureDoesNotDisplaceTheRunsOwnError: when the run has
// already failed, that failure is the one worth hearing about; the write error
// is logged beside it rather than put in its place.
func TestReportWriteFailureDoesNotDisplaceTheRunsOwnError(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nSee [it](ac:Nowhere).\n")

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:        filepath.Join(dir, "doc.md"),
		Features:     []string{"mention"},
		CheckLinks:   []string{"confluence"},
		OutputFormat: report.FormatJSON,
		Output:       failingWriter{},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nowhere", "the unresolved link is still the complaint")
	assert.NotContains(t, err.Error(), "unable to write the run report")
	assert.Contains(t, logged.String(), "unable to write the run report",
		"the failed write is still said, in the log")
	assert.Contains(t, logged.String(), "disk full")
}

// runReport runs over files in a fresh space with the given format, letting
// adjust change the config first, and returns what the run wrote and returned.
func runReport(t *testing.T, format string, files map[string]string, adjust func(*Config)) (string, error) {
	t.Helper()

	server := outputServer(t)
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}

	var out bytes.Buffer
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		OutputFormat: format, Output: &out,
	}
	if adjust != nil {
		adjust(&config)
	}

	err := Run(config)

	return out.String(), err
}

// errorLines returns the ::error annotations in GitHub output.
func errorLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "::error") {
			lines = append(lines, line)
		}
	}

	return lines
}

var unresolvedLink = map[string]string{
	"doc.md": outHeader + "<!-- Title: Doc -->\n\nSee [it](ac:Nowhere).\n",
}

func checkConfluenceLinks(config *Config) {
	config.CheckLinks = []string{"confluence"}
}

// TestReportCarriesAnUnresolvedLink: the run failed on the link, and the
// report written on the way out listed the page as published and said nothing
// else -- a script reading it saw a clean run with a non-zero exit.
func TestReportCarriesAnUnresolvedLink(t *testing.T) {
	out, err := runReport(t, report.FormatJSON, unresolvedLink, checkConfluenceLinks)
	require.Error(t, err)

	var parsed report.Report
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))

	require.Len(t, parsed.Errors, 1, "the failure is recorded once")
	assert.Equal(t, err.Error(), parsed.Errors[0], "and it is the failure the run returned")
	assert.Contains(t, parsed.Errors[0], `"ac:Nowhere"`)

	require.Len(t, parsed.Pages, 1)
	assert.Equal(t, report.StatusPublished, parsed.Pages[0].Status)
}

// TestGitHubAnnotatesAnUnresolvedLink is the same run in the other format:
// the one error annotation there was is the one a pull request shows.
func TestGitHubAnnotatesAnUnresolvedLink(t *testing.T) {
	out, err := runReport(t, report.FormatGitHub, unresolvedLink, checkConfluenceLinks)
	require.Error(t, err)

	lines := errorLines(out)
	require.Len(t, lines, 1, out)
	assert.True(t, strings.HasPrefix(lines[0], "::error::1 link does not resolve:%0A"), lines[0])
	assert.Contains(t, lines[0], "Nowhere")
}

// TestReportDoesNotRepeatAFailedDocument: a document's failure is recorded
// against the document, so neither it nor the "one or more files failed" that
// sums it up is said a second time as a failure of the run.
func TestReportDoesNotRepeatAFailedDocument(t *testing.T) {
	files := map[string]string{
		"good.md": outHeader + "<!-- Title: Good -->\n\nFine.\n",
		"bad.md":  outHeader + "<!-- Title: Bad -->\n\n<!-- ac:ignore -->\nunclosed\n",
	}

	for _, continueOnError := range []bool{false, true} {
		out, err := runReport(t, report.FormatJSON, files, func(config *Config) {
			config.ContinueOnError = continueOnError
		})
		require.Error(t, err)

		var parsed report.Report
		require.NoError(t, json.Unmarshal([]byte(out), &parsed))

		assert.Empty(t, parsed.Errors, "continue-on-error=%v", continueOnError)

		var failed int
		for _, page := range parsed.Pages {
			if page.Status == report.StatusFailed {
				failed++
			}
		}
		assert.Equal(t, 1, failed, "continue-on-error=%v", continueOnError)
	}
}

// refuseManifestWrites lets every read through and refuses every write to a
// property, which is where the manifest is kept, so a run gets as far as
// saving it and no further.
func refuseManifestWrites(server *confluencetest.Server) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet || !strings.Contains(r.URL.Path, "propert") {
			return 0, "", false
		}

		return http.StatusForbidden, `{"message":"nope"}`, true
	})
}

// TestReportCarriesAFailedManifestSave: the pages published, the mapping of
// them did not, and the run failed saying so -- in the log, and nowhere a
// script reading the report would look.
func TestReportCarriesAFailedManifestSave(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", outHeader+"<!-- Title: Doc -->\n\nFine.\n")

	refuseManifestWrites(server)

	for _, format := range []string{report.FormatJSON, report.FormatGitHub} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			err := Run(Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
				TrackPages: true, OutputFormat: format, Output: &out,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "unable to save page manifest")

			switch format {
			case report.FormatJSON:
				var parsed report.Report
				require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))

				// Once, although the save is tried again on the way out and
				// fails again there.
				require.Len(t, parsed.Errors, 1, out.String())
				assert.Equal(t, err.Error(), parsed.Errors[0])

			case report.FormatGitHub:
				lines := errorLines(out.String())
				require.Len(t, lines, 1, out.String())
				assert.True(t, strings.HasPrefix(lines[0], "::error::unable to save page manifest"), lines[0])
			}
		})
	}
}

// TestReportCarriesAFailedSaveBesideAnUnresolvedLink: the run returns the
// link, and the manifest that was not saved used to go unsaid everywhere but
// the log.
func TestReportCarriesAFailedSaveBesideAnUnresolvedLink(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", unresolvedLink["doc.md"])

	refuseManifestWrites(server)

	var out bytes.Buffer
	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"confluence"}, TrackPages: true,
		OutputFormat: report.FormatJSON, Output: &out,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nowhere")

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Len(t, parsed.Errors, 2, out.String())
	assert.Contains(t, parsed.Errors[0], "unable to save page manifest")
	assert.Equal(t, err.Error(), parsed.Errors[1])
}

// TestReportCarriesASaveThatFailedOnTheWayOut: a run stopped by a document
// never reaches the explicit save, and the one tried on the way out failed
// into the log alone.
func TestReportCarriesASaveThatFailedOnTheWayOut(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "a-good.md", outHeader+"<!-- Title: Good -->\n\nFine.\n")
	writeFile(t, dir, "z-bad.md", outHeader+"<!-- Title: Bad -->\n\n<!-- ac:ignore -->\nunclosed\n")

	refuseManifestWrites(server)

	var out bytes.Buffer
	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OutputFormat: report.FormatJSON, Output: &out,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "z-bad.md", "the document is still the complaint")

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Len(t, parsed.Errors, 1, out.String())
	assert.Contains(t, parsed.Errors[0], "unable to save page manifest")
}
