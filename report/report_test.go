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

// TestGitHubAnnotatesAWarningAgainstAPageThatPublished covers a document that
// was published with something wrong in it, which is what --check-links-warn-only
// asks for: told about a link that does not resolve, without the run failing.
//
// The warning has to be an annotation of its own. Written only to the log it is
// a line in the build output that nobody scrolls to, which is the opposite of
// what asking to be warned was for.
func TestGitHubAnnotatesAWarningAgainstAPageThatPublished(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusPublished, Title: "A", URL: "https://example/x/1",
		Warnings: []string{`link "guide" does not resolve: it is a directory, not a document`},
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, `::notice file=docs/a.md::published "A" to https://example/x/1`, lines[0])
	assert.Equal(t,
		`::warning file=docs/a.md::link "guide" does not resolve: it is a directory, not a document`,
		lines[1])
}

// TestGitHubAnnotatesAWarningOnAPageWithNoLineOfItsOwn is the case that would
// otherwise disappear entirely: a document nothing was written for, because it
// had not changed, still carrying a link that does not resolve.
func TestGitHubAnnotatesAWarningOnAPageWithNoLineOfItsOwn(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusUnchanged, Title: "A",
		Warnings: []string{"link \"guide\" does not resolve: there is no such file"},
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	got := strings.TrimSpace(out.String())
	assert.Equal(t,
		`::warning file=docs/a.md::link "guide" does not resolve: there is no such file`, got)
}

// TestJSONCarriesWarnings covers the other machine-readable format, so that
// what a run warned about can be read without parsing the log.
func TestJSONCarriesWarnings(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusPublished, Warnings: []string{"a warning"}})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatJSON))

	assert.Contains(t, out.String(), `"warnings"`)
	assert.Contains(t, out.String(), `"a warning"`)
}

// TestGitHubSaysNothingExtraWithoutWarnings is the boundary: the field is
// absent for almost every document, and must add nothing when it is.
func TestGitHubSaysNothingExtraWithoutWarnings(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusUnchanged, Title: "A"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Empty(t, strings.TrimSpace(out.String()))
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
