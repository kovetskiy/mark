package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarningsRecordKindAndFile(t *testing.T) {
	w := NewWarnings("docs/a.md")
	w.Add(WarningUnknownKey, "front matter key %q is not read by mark and was ignored", "colour")
	w.Add(WarningAttachment, "unused attachment: %s", "logo.png")

	assert.Equal(t, []Warning{
		{Kind: WarningUnknownKey, File: "docs/a.md", Message: `front matter key "colour" is not read by mark and was ignored`},
		{Kind: WarningAttachment, File: "docs/a.md", Message: "unused attachment: logo.png"},
	}, w.List())
}

// TestWarningsRecordEachOnceOverRereads: a warning a read adds again is kept,
// as another occurrence; one a later read only repeats is not.
func TestWarningsRecordEachOnceOverRereads(t *testing.T) {
	w := NewWarnings("docs/a.md")
	w.Add(WarningColumnWidths, "not applied")
	w.Add(WarningColumnWidths, "not applied")
	w.Add(WarningUnknownKey, "not applied")
	require.Len(t, w.List(), 3)

	w.Reread()
	w.Add(WarningColumnWidths, "not applied")
	w.Add(WarningColumnWidths, "not applied")
	w.Add(WarningUnknownKey, "not applied")
	assert.Len(t, w.List(), 3)

	w.Add(WarningInclude, "only the second read found this")
	assert.Len(t, w.List(), 4)
}

// TestWarningsMapLines: a line counted in text with lines taken out is given
// as the line of the file.
func TestWarningsMapLines(t *testing.T) {
	w := NewWarnings("docs/a.md")
	w.MapLines(func(line int) int { return line + 5 })
	w.AddAt(3, WarningStrandedHeader, "stranded")
	w.Add(WarningAttachment, "no line")

	assert.Equal(t, 8, w.List()[0].Line)
	assert.Equal(t, 0, w.List()[1].Line)
}

func TestNilWarningsOnlyLog(t *testing.T) {
	var w *Warnings
	w.Add(WarningUnknownKey, "logged, not kept")

	assert.Nil(t, w.List())
}

func TestListIsACopy(t *testing.T) {
	w := NewWarnings("a.md")
	w.Add(WarningUnknownKey, "one")

	list := w.List()
	list[0].Message = "changed"

	assert.Equal(t, "one", w.List()[0].Message)
}

// TestJSONKeepsWarningsAsStrings: the structured warnings sit beside the
// strings a v17 consumer already reads, rather than replacing them.
func TestJSONKeepsWarningsAsStrings(t *testing.T) {
	w := NewWarnings("docs/a.md")
	w.Add(WarningAttachment, "unused attachment: %s", "logo.png")

	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusPublished,
		Warnings: []string{"unused attachment: logo.png"}, WarningDetails: w.List(),
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatJSON))

	var got struct {
		Pages []struct {
			Warnings       []string  `json:"warnings"`
			WarningDetails []Warning `json:"warningDetails"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.String()), &got))
	require.Len(t, got.Pages, 1)

	assert.Equal(t, []string{"unused attachment: logo.png"}, got.Pages[0].Warnings)
	assert.Equal(t, []Warning{
		{Kind: WarningAttachment, File: "docs/a.md", Message: "unused attachment: logo.png"},
	}, got.Pages[0].WarningDetails)
}

// TestGitHubAnnotatesTheLineOfAWarning: a warning about one line is annotated
// at it, unless it goes to a source, where that line means nothing.
func TestGitHubAnnotatesTheLineOfAWarning(t *testing.T) {
	r := New()
	w := NewWarnings("docs/a.md")
	w.AddAt(4, WarningUnknownKey, "front matter key %q is not read by mark and was ignored", "colour")
	w.Add(WarningAttachment, "unused attachment: %s", "logo.png")
	r.AddPage(Page{File: "docs/a.md", Status: StatusUnchanged, WarningDetails: w.List()})

	r.AddPage(Page{File: "build/b.md", Status: StatusUnchanged, WarningDetails: []Warning{
		{Kind: WarningStrandedHeader, File: "build/b.md", Line: 9, Message: "stranded"},
	}})
	r.SetSources(func(file string) string {
		if file == "build/b.md" {
			return "src/b.md.tmpl"
		}

		return ""
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::warning file=docs/a.md,line=4::front matter key \"colour\" is not read by mark and was ignored\n"+
			"::warning file=docs/a.md::unused attachment: logo.png\n"+
			"::warning file=src/b.md.tmpl::stranded\n",
		out.String())
	assert.Equal(t, 4, r.Pages[0].WarningDetails[0].Line, "the json report keeps the line")
}

// TestGitHubLeavesOutTheWarningsOfAnUnsynchronizedDocument: a document skipped
// because it asked to be is left out of the pull request entirely, warnings
// included; the json report still has them.
func TestGitHubLeavesOutTheWarningsOfAnUnsynchronizedDocument(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/draft.md", Status: StatusSkipped, Reason: ReasonNotSynchronized,
		Warnings:       []string{"front matter key \"colour\" is not read by mark and was ignored"},
		WarningDetails: []Warning{{Kind: WarningUnknownKey, File: "docs/draft.md", Message: "front matter key \"colour\" is not read by mark and was ignored"}},
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))
	assert.Empty(t, out.String())

	out.Reset()
	require.NoError(t, r.Write(&out, FormatJSON))
	assert.Contains(t, out.String(), `"kind": "unknown-key"`)
}

// TestGitHubAnnotatesAWarningAgainstItsOwnFile: a warning names the file it is
// about, which the annotation follows, falling back to the page's.
func TestGitHubAnnotatesAWarningAgainstItsOwnFile(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusUnchanged,
		Warnings: []string{"named", "unnamed"},
		WarningDetails: []Warning{
			{Kind: WarningUnknownKey, File: "docs/b.md", Message: "named"},
			{Kind: WarningUnknownKey, Message: "unnamed"},
		},
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::warning file=docs/b.md::named\n::warning file=docs/a.md::unnamed\n",
		out.String())
}

// TestGitHubAnnotatesAWarningAgainstTheSource: a warning is annotated against
// the source of the file it is about -- the document's, or another file's --
// and against that file itself when it has no source.
func TestGitHubAnnotatesAWarningAgainstTheSource(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "build/a.md", Status: StatusUnchanged,
		Warnings: []string{"about the document", "about a fragment", "about a shared file"},
		WarningDetails: []Warning{
			{Kind: WarningUnknownKey, File: "build/a.md", Message: "about the document"},
			{Kind: WarningInclude, File: "build/frag.md", Message: "about a fragment"},
			{Kind: WarningInclude, File: "shared/b.md", Message: "about a shared file"},
		},
	})
	r.SetSources(func(file string) string {
		if !strings.HasPrefix(file, "build/") {
			return ""
		}

		return "src/" + strings.TrimPrefix(file, "build/") + ".tmpl"
	})

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::warning file=src/a.md.tmpl::about the document\n"+
			"::warning file=src/frag.md.tmpl::about a fragment\n"+
			"::warning file=shared/b.md::about a shared file\n",
		out.String())
	assert.Equal(t, "src/frag.md.tmpl", r.Pages[0].WarningDetails[1].Source)
}

// TestSetWarningsReplacesAPagesWarnings: a warning found after a document's
// line was written replaces its warnings in both forms, and a file with no
// line is left out.
func TestSetWarningsReplacesAPagesWarnings(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "docs/a.md", Status: StatusPublished,
		Warnings:       []string{"old"},
		WarningDetails: []Warning{{Kind: WarningUnknownKey, File: "docs/a.md", Message: "old"}},
	})

	updated := []Warning{
		{Kind: WarningUnknownKey, File: "docs/a.md", Message: "old"},
		{Kind: WarningUnresolvedLink, File: "docs/a.md", Message: "new"},
	}
	r.SetWarnings("docs/a.md", updated)
	r.SetWarnings("docs/missing.md", updated)

	require.Len(t, r.Pages, 1)
	assert.Equal(t, updated, r.Pages[0].WarningDetails)
	assert.Equal(t, []string{"old", "new"}, r.Pages[0].Warnings)
}
