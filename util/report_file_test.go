package util

import (
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

// reportFileFixture is a server with a space to publish to and a document to
// publish there.
func reportFileFixture(t *testing.T) (*confluencetest.Server, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	document := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	return server, document
}

// TestReportFileFlag: --report-file, from the command line, the environment or
// the configuration file, takes the json report off standard output, which
// keeps the URL of the page published. A report an earlier run left there is
// replaced.
func TestReportFileFlag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    bool
		config bool
	}{
		{name: "flag"},
		{name: "environment", env: true},
		{name: "configuration file", config: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noDefaultConfig(t)
			server, document := reportFileFixture(t)

			reportFile := filepath.Join(t.TempDir(), "report.json")
			require.NoError(t, os.WriteFile(reportFile, []byte(strings.Repeat("stale ", 1000)), 0o600))

			var configContent string
			args := []string{"--color", "never", "-u", "user", "-p", "token", "publish",
				"-b", server.URL, "--output-format", "json", "--files", document}
			switch {
			case tc.env:
				t.Setenv("MARK_REPORT_FILE", reportFile)
			case tc.config:
				configContent = "report-file = \"" + filepath.ToSlash(reportFile) + "\"\n"
			default:
				args = append(args, "--report-file", reportFile)
			}
			config := writeConfig(t, t.TempDir(), "mark.toml", configContent)

			out, _, err := runMark(t, append([]string{"-c", config}, args...)...)
			require.NoError(t, err)

			assert.Equal(t, server.URL+"/display/DOCS/1003\n", out, "standard output keeps the URL")

			written, err := os.ReadFile(reportFile)
			require.NoError(t, err)

			var parsed report.Report
			require.NoError(t, json.Unmarshal(written, &parsed), string(written))
			require.Len(t, parsed.Pages, 1)
			assert.Equal(t, report.StatusPublished, parsed.Pages[0].Status)
		})
	}
}

// TestReportFileFlagUnderCompileOnly: the github report goes to the file and
// the compiled body alone to standard output.
func TestReportFileFlagUnderCompileOnly(t *testing.T) {
	noDefaultConfig(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	require.NoError(t, os.WriteFile(good, []byte("<!-- Space: DOCS -->\n<!-- Title: Good -->\n\nBody.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Bad -->\n\n<!-- ac:ignore -->\nunclosed\n"), 0o600))
	reportFile := filepath.Join(t.TempDir(), "report.txt")

	out, _, err := runMark(t, "--color", "never", "publish", "--compile-only", "--continue-on-error",
		"--output-format", "github", "--report-file", reportFile, "--files", filepath.Join(dir, "*.md"))
	require.Error(t, err, "bad.md fails")

	assert.Equal(t, "<p>Body.</p>\n\n", out)

	written, err := os.ReadFile(reportFile)
	require.NoError(t, err)
	assert.Contains(t, string(written), "::error file="+filepath.Join(dir, "bad.md"))
	assert.NotContains(t, string(written), "<p>")
}

// TestReportFileFlagDashIsStandardOutput: "-" writes the report to standard
// output, as without the flag, so the URLs give way to it there.
func TestReportFileFlagDashIsStandardOutput(t *testing.T) {
	noDefaultConfig(t)
	server, document := reportFileFixture(t)

	out, _, err := runMark(t, "--color", "never", "-u", "user", "-p", "token", "publish",
		"-b", server.URL, "--output-format", "json", "--report-file", "-", "--files", document)
	require.NoError(t, err)

	var parsed report.Report
	require.NoError(t, json.Unmarshal([]byte(out), &parsed), out)
	require.Len(t, parsed.Pages, 1)
}

// TestReportFileFlagRefusesAnUnwritablePath: a file that cannot be created
// fails the run before anything is published, naming the flag.
func TestReportFileFlagRefusesAnUnwritablePath(t *testing.T) {
	noDefaultConfig(t)
	server, document := reportFileFixture(t)

	_, _, err := runMark(t, "--color", "never", "-u", "user", "-p", "token", "publish",
		"-b", server.URL, "--report-file", filepath.Join(t.TempDir(), "missing", "report.json"),
		"--files", document)
	require.ErrorContains(t, err, "unable to create --report-file")
	assert.Zero(t, server.CountRequests("POST", "/content"), "nothing is published")
}

// TestReportFileFlagEmptiedWhenTheRunIsRefused: a run refused for its
// settings, here for want of a base URL, still empties the file, so that
// the report an earlier run left there is not read as this one's.
func TestReportFileFlagEmptiedWhenTheRunIsRefused(t *testing.T) {
	noDefaultConfig(t)
	_, document := reportFileFixture(t)

	reportFile := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(reportFile, []byte(`{"pages": []}`), 0o600))

	_, _, err := runMark(t, "--color", "never", "-u", "user", "-p", "token", "publish",
		"--output-format", "json", "--report-file", reportFile, "--files", document)
	require.ErrorContains(t, err, "base URL")

	written, err := os.ReadFile(reportFile)
	require.NoError(t, err)
	assert.Empty(t, written)
}
