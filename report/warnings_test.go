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

func TestWarningsRecordEachOnce(t *testing.T) {
	w := NewWarnings("docs/a.md")
	w.Add(WarningMissingPage, "page %s no longer exists", "1")
	w.Add(WarningMissingPage, "page %s no longer exists", "1")
	w.Add(WarningUnknownKey, "page %s no longer exists", "1")

	assert.Len(t, w.List(), 2)
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

// TestGitHubAnnotatesAWarningAgainstTheSource: a warning about the document is
// annotated where its status is, against the source it was made from; one
// about another file stays against that file.
func TestGitHubAnnotatesAWarningAgainstTheSource(t *testing.T) {
	r := New()
	r.AddPage(Page{
		File: "build/a.md", Status: StatusUnchanged,
		Warnings: []string{"about the document", "about a fragment"},
		WarningDetails: []Warning{
			{Kind: WarningUnknownKey, File: "build/a.md", Message: "about the document"},
			{Kind: WarningUnknownKey, File: "build/frag.md", Message: "about a fragment"},
		},
	})
	r.SetSources(func(file string) string { return "src/a.md.tmpl" })

	var out strings.Builder
	require.NoError(t, r.Write(&out, FormatGitHub))

	assert.Equal(t,
		"::warning file=src/a.md.tmpl::about the document\n"+
			"::warning file=build/frag.md::about a fragment\n",
		out.String())
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
