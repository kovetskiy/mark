package mark

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
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
