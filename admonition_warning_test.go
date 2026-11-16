package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strandedBody is a document with one warning: an admonition on line 8 whose
// body, written level with it and ended by no closing line, is published
// after it. The line counts through the headers and through a region that is
// not published.
var strandedBody = map[string]string{
	"a.md": outHeader + "<!-- Title: A -->\n\n" +
		"<!-- ac:ignore -->\nNot published.\n<!-- ac:ignore end -->\n" +
		"!!! note \"Remember\"\nText.\n",
}

// TestAnAdmonitionBodyLeftOutIsReported: a run that publishes an admonition
// without the lines written as its body says so in its report, against the
// document and the line, whether it publishes, compiles or runs dry -- and
// still publishes the page.
func TestAnAdmonitionBodyLeftOutIsReported(t *testing.T) {
	cases := []struct {
		name   string
		mode   ModeConfig
		status report.Status
	}{
		{"publish", ModeConfig{}, report.StatusPublished},
		{"compile only", ModeConfig{CompileOnly: true, Offline: true}, report.StatusCompiled},
		{"dry run", ModeConfig{DryRun: true}, report.StatusWouldPublish},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runReport(t, report.FormatJSON, strandedBody, func(config *Config) {
				config.Mode = tc.mode
				config.Rendering.Features = []string{"mkdocsadmonitions"}
			})
			require.NoError(t, err, "a warning does not fail the run")

			got := decodeReport(t, []byte(out))
			require.Len(t, got.Pages, 1)

			page := got.Pages[0]
			assert.Equal(t, tc.status, page.Status)

			require.Len(t, page.WarningDetails, 1, "%+v", page.WarningDetails)

			warning := page.WarningDetails[0]
			assert.Equal(t, report.WarningAdmonitionBody, warning.Kind)
			assert.Equal(t, page.File, warning.File)
			assert.Equal(t, 8, warning.Line)
			assert.Contains(t, warning.Message, `the lines after "!!! note \"Remember\"" are not indented`)
			assert.Equal(t, []string{warning.Message}, page.Warnings)
		})
	}
}

// TestAnAdmonitionBodyLeftOutIsAnnotated: the github format puts the warning
// on the admonition's line.
func TestAnAdmonitionBodyLeftOutIsAnnotated(t *testing.T) {
	out, err := runReport(t, report.FormatGitHub, strandedBody, func(config *Config) {
		config.Mode = ModeConfig{CompileOnly: true, Offline: true}
		config.Rendering.Features = []string{"mkdocsadmonitions"}
	})
	require.NoError(t, err)

	annotations := commandLines(out, "::")
	require.Len(t, annotations, 1, out)
	assert.Contains(t, annotations[0], "::warning file=")
	assert.Contains(t, annotations[0], "a.md,line=8::")
}

// TestAnAdmonitionBodyIsNotReportedWithoutTheFeature: with the feature off
// the opening line is text, and the report has nothing to say about it.
func TestAnAdmonitionBodyIsNotReportedWithoutTheFeature(t *testing.T) {
	out, err := runReport(t, report.FormatJSON, strandedBody, func(config *Config) {
		config.Mode = ModeConfig{CompileOnly: true, Offline: true}
	})
	require.NoError(t, err)

	got := decodeReport(t, []byte(out))
	require.Len(t, got.Pages, 1)
	assert.Empty(t, got.Pages[0].WarningDetails)
}
