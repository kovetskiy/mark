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

	_, document := publishable(t)

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
