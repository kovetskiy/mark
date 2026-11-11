package mark

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// splitRun runs a.md, which publishes, and b.md, which fails, against a fresh
// server, with the report going to a writer of its own when separate is set.
// It returns what reached PublishConfig.Output and ReportConfig.Output, with
// the server's address and the directory taken out so that two runs compare.
func splitRun(t *testing.T, format report.Format, mode ModeConfig, update UpdateConfig, separate bool) (string, string, error) {
	t.Helper()

	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nA.\n")
	writeFile(t, dir, "b.md", outHeader+"<!-- Title: B -->\n\n<!-- ac:ignore -->\nunclosed\n")

	var out, reportOut strings.Builder
	config := Config{
		PublishConfig: PublishConfig{
			Mode:      mode,
			Update:    update,
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Output:    &out,
		},
		Files:           filepath.Join(dir, "*.md"),
		Connection:      ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Report:          ReportConfig{OutputFormat: format},
		ContinueOnError: true,
	}
	if separate {
		config.Report.Output = &reportOut
	}

	err := Run(context.Background(), config)

	anonymous := strings.NewReplacer(server.URL, "SERVER", dir, "DIR")

	return anonymous.Replace(out.String()), anonymous.Replace(reportOut.String()), err
}

// TestReportOutputSeparatesTheReport: with a writer of its own the report goes
// there in every format and mode, a run that fails included, and
// PublishConfig.Output keeps the URLs and the compiled bodies. Nothing is lost
// or said twice: without it, the one stream holds exactly what the two hold
// apart, less the URLs and bodies a json or github report displaces.
func TestReportOutputSeparatesTheReport(t *testing.T) {
	modes := []struct {
		name   string
		mode   ModeConfig
		update UpdateConfig
	}{
		{"publish", ModeConfig{}, UpdateConfig{}},
		{"dry run", ModeConfig{DryRun: true}, UpdateConfig{}},
		{"compile-only", ModeConfig{CompileOnly: true, Offline: true}, UpdateConfig{}},
		{"changes-only dry run", ModeConfig{DryRun: true}, UpdateConfig{ChangesOnly: true}},
	}

	for _, format := range []report.Format{report.FormatURL, report.FormatJSON, report.FormatGitHub} {
		for _, m := range modes {
			t.Run(string(format)+"/"+m.name, func(t *testing.T) {
				out, reportOut, err := splitRun(t, format, m.mode, m.update, true)
				var failed *FilesFailedError
				require.ErrorAs(t, err, &failed, "b.md fails")

				shared, nothing, err := splitRun(t, format, m.mode, m.update, false)
				require.ErrorAs(t, err, &failed)
				require.Empty(t, nothing)

				published := m.mode == ModeConfig{}
				url := "SERVER/display/DOCS/1004\n"

				// Output: the URL of the page published, or the body of the
				// page previewed, whatever the format.
				if published {
					assert.Equal(t, url, out)
				} else {
					assert.Equal(t, 1, strings.Count(out, "<p>A.</p>"), out)
					assert.NotContains(t, out, "::")
					assert.NotContains(t, out, `"pages"`)
				}

				switch format {
				case report.FormatURL:
					if published {
						assert.Equal(t, url, reportOut, "the url format's report is its URLs")
					} else {
						assert.Empty(t, reportOut, "a preview publishes no URL")
					}

					assert.Equal(t, out, shared)

					return

				case report.FormatJSON:
					var parsed report.Report
					require.NoError(t, json.Unmarshal([]byte(reportOut), &parsed), reportOut)
					assert.Contains(t, reportOut, `"file": "DIR/b.md"`)
					assert.Contains(t, reportOut, `"status": "failed"`)

				case report.FormatGitHub:
					for _, line := range strings.Split(strings.TrimSuffix(reportOut, "\n"), "\n") {
						assert.True(t, strings.HasPrefix(line, "::"), "every line is a workflow command: %q", line)
					}
					assert.Contains(t, reportOut, "::error file=DIR/b.md::")
				}

				if published {
					assert.Contains(t, reportOut, "SERVER/display/DOCS/1004", "the report names the page too")
				}

				assert.Equal(t, reportOut, shared,
					"the URLs and the bodies give way to a report on the same stream")
			})
		}
	}
}

// TestReportOutputWriteFailureFailsTheRun: a report that cannot be written to
// its own writer fails the run, as one that cannot be written to
// PublishConfig.Output does, and the URL is still on PublishConfig.Output.
func TestReportOutputWriteFailureFailsTheRun(t *testing.T) {
	for _, format := range []report.Format{report.FormatURL, report.FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			server := outputServer(t)
			dir := t.TempDir()
			writeFile(t, dir, "a.md", outHeader+"<!-- Title: A -->\n\nA.\n")

			var out strings.Builder
			err := Run(context.Background(), Config{
				PublishConfig: PublishConfig{
					Rendering: RenderingConfig{Features: []string{"mention"}},
					Output:    &out,
				},
				Files:      filepath.Join(dir, "*.md"),
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
				Report:     ReportConfig{OutputFormat: format, Output: failingWriter{}},
			})

			require.ErrorContains(t, err, "unable to write the run report")
			assert.Contains(t, out.String(), "/display/DOCS/1004")
		})
	}
}
