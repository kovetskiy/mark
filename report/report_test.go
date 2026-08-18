package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormat(t *testing.T) {
	for value, expected := range map[string]string{
		"": FormatURL, "url": FormatURL, "json": FormatJSON,
		"github": FormatGitHub, " GitHub ": FormatGitHub,
	} {
		got, err := ParseFormat(value)
		assert.NoError(t, err, "value %q", value)
		assert.Equal(t, expected, got, "value %q", value)
	}

	_, err := ParseFormat("yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github")
}

func TestJSONDescribesTheRun(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusPublished, Space: "DOCS",
		Title: "A", PageID: "1004", URL: "https://example/x/1004",
	})
	r.AddOrphan(Orphan{File: "docs/old.md", Title: "Old", Action: "delete"})
	r.AddError("something went wrong")

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatJSON))

	got := out.String()
	assert.Contains(t, got, `"file": "docs/a.md"`)
	assert.Contains(t, got, `"status": "published"`)
	assert.Contains(t, got, `"pageId": "1004"`)
	assert.Contains(t, got, `"action": "delete"`)
	assert.Contains(t, got, `"something went wrong"`)
}

// TestGitHubAnnotatesTheFile is the point of that format: a failure has to name
// the file so it appears against it in a pull request.
func TestGitHubAnnotatesTheFile(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusPublished, Title: "A", URL: "https://example/x/1"})
	r.AddPage(Page{File: "docs/b.md", Status: StatusFailed, Reason: "unable to compile markdown"})
	r.AddPage(Page{File: "docs/c.md", Status: StatusSkipped, Reason: "the document is not synchronized"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, `::notice file=docs/a.md::published "A" to https://example/x/1`, lines[0])
	assert.Equal(t, "::error file=docs/b.md::unable to compile markdown", lines[1])
	assert.Equal(t, "::warning file=docs/c.md::the document is not synchronized", lines[2])
}

// TestGitHubEscapes covers the characters that would otherwise end a command
// early or start another one.
func TestGitHubEscapes(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File:   "docs/a,b:c.md",
		Status: StatusFailed,
		Reason: "100% wrong\nand on two lines",
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	got := strings.TrimSpace(out.String())
	assert.Equal(t, "::error file=docs/a%2Cb%3Ac.md::100%25 wrong%0Aand on two lines", got)
	assert.Equal(t, 1, len(strings.Split(got, "\n")),
		"a newline in a message must not become a second command")
}

// TestURLFormatWritesNothingAtTheEnd: those lines are printed as each page
// publishes, and repeating them would double mark's usual output.
func TestURLFormatWritesNothingAtTheEnd(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusPublished, URL: "https://example/x/1"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatURL))
	assert.Empty(t, out.String())
}

// TestAPageIsReportedOnce: a document published twice -- once waiting on a page
// this run created, then again once it existed -- is one document.
func TestAPageIsReportedOnce(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusPublished, URL: "first"})
	r.AddPage(Page{File: "docs/a.md", Status: StatusPublished, URL: "second"})

	require.Len(t, r.Pages, 1)
	assert.Equal(t, "second", r.Pages[0].URL, "the later word is the true one")
}
