package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormat(t *testing.T) {
	for value, expected := range map[string]Format{
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
	r.AddPage(Page{File: "docs/c.md", Status: StatusSkipped, Reason: "edited in Confluence since mark published it"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, `::notice file=docs/a.md::published "A" to https://example/x/1`, lines[0])
	assert.Equal(t, "::error file=docs/b.md::unable to compile markdown", lines[1])
	assert.Equal(t, "::warning file=docs/c.md::edited in Confluence since mark published it", lines[2])
}

// TestGitHubLeavesOutAnUnsynchronizedDocument: a document that opted out of
// publishing was skipped on purpose, so the pull request is not told about it.
// The json report still lists it.
func TestGitHubLeavesOutAnUnsynchronizedDocument(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/draft.md", Status: StatusSkipped, Reason: ReasonNotSynchronized})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))
	assert.Empty(t, out.String())

	out.Reset()
	require.NoError(t, r.Write(&out, FormatJSON))
	assert.Contains(t, out.String(), ReasonNotSynchronized)
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

func TestGitHubSaysWhatADryRunWouldDo(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "docs/a.md", Status: StatusWouldCreate, Title: "A"})
	r.AddPage(Page{File: "docs/b.md", Status: StatusWouldUpdate, Title: "B"})
	r.AddPage(Page{File: "docs/c.md", Status: StatusUnchanged, Title: "C"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::notice file=docs/a.md::would create \"A\"\n::notice file=docs/b.md::would update \"B\"\n",
		out.String())
}

// TestSetSourcesAnnotatesTheSource: a generated document is annotated against
// the file it was generated from, orphans included, and against its own file
// when it has no source; a nil source leaves them alone.
func TestSetSourcesAnnotatesTheSource(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "build/a.md", Status: StatusFailed, Reason: "broken"})
	r.AddPage(Page{File: "build/b.md", Status: StatusFailed, Reason: "also broken"})
	r.AddOrphan(Orphan{File: "build/gone.md", Title: "Gone", Action: OnOrphanReport})

	r.SetSources(func(file string) string {
		if file == "build/b.md" {
			return ""
		}

		return strings.Replace(file, "build/", "src/", 1)
	})
	r.SetSources(nil)

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::error file=src/a.md::broken\n"+
			"::error file=build/b.md::also broken\n"+
			"::warning file=src/gone.md::page \"Gone\" has no source file\n",
		out.String())
}

// TestJSONKeepsTheFileBesideItsSource: the source is added, never put in place
// of the file mark read.
func TestJSONKeepsTheFileBesideItsSource(t *testing.T) {
	r := New()
	r.AddPage(Page{File: "build/a.md", Status: StatusPublished})
	r.AddPage(Page{File: "build/b.md", Status: StatusPublished})
	r.SetSources(func(file string) string {
		if file == "build/b.md" {
			return file
		}

		return "src/a.md.tmpl"
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatJSON))

	var parsed Report
	require.NoError(t, json.Unmarshal([]byte(out.String()), &parsed))
	require.Len(t, parsed.Pages, 2)
	assert.Equal(t, "build/a.md", parsed.Pages[0].File)
	assert.Equal(t, "src/a.md.tmpl", parsed.Pages[0].Source)
	assert.Equal(t, 1, strings.Count(out.String(), `"source"`), "a page that is its own source has no source field")
	assert.Equal(t, StatusPublished, r.StatusOf("build/a.md"), "pages are still found by the file mark read")
}

func TestSourceOf(t *testing.T) {
	source := func(file string) string {
		if file == "build/a.md" {
			return "src/a.md.tmpl"
		}

		return file
	}

	assert.Equal(t, "src/a.md.tmpl", SourceOf("build/a.md", source))
	assert.Empty(t, SourceOf("docs/b.md", source), "a file that is its own source has none")
	assert.Empty(t, SourceOf("build/a.md", nil))
}

// TestUntrackedSpaceIsReported: a space published to without its page
// manifest is in the JSON report, and a warning of the run in the github one.
func TestUntrackedSpaceIsReported(t *testing.T) {
	r := New()
	r.AddUntracked(Untracked{Space: "DOCS", ManifestPage: "Handbook"})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatJSON))
	var parsed struct {
		Untracked []map[string]string `json:"untracked"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.String()), &parsed))
	assert.Equal(t, []map[string]string{{"space": "DOCS", "manifestPage": "Handbook"}}, parsed.Untracked)

	out.Reset()
	require.NoError(t, r.Write(&out, FormatGitHub))
	assert.Equal(t,
		"::warning::space \"DOCS\" was published untracked: no page mapping was saved on its manifest page \"Handbook\"\n",
		out.String())
}
