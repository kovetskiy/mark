package mark

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
