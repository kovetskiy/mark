package metadata

import (
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWarningKindsTellTheCasesApart: a key mark does not read, a value its key
// cannot hold and a header below the opening comments each have a kind of
// their own, so a caller can single one out without reading the message.
func TestWarningKindsTellTheCasesApart(t *testing.T) {
	document := "---\n" +
		"title: Doc\n" +
		"space: DOCS\n" +
		"colour: blue\n" +
		"labels: [one, 2]\n" +
		"---\n\n" +
		"Text.\n\n" +
		"<!-- Label: late -->\n"

	warnings := report.NewWarnings("doc.md")
	_, _, err := ExtractMeta([]byte(document), ExtractOptions{
		Filename: "doc.md", FrontMatter: true, Warnings: warnings,
	})
	require.NoError(t, err)

	kinds := map[report.WarningKind]int{}
	for _, warning := range warnings.List() {
		assert.Equal(t, "doc.md", warning.File)
		kinds[warning.Kind]++
	}

	assert.Equal(t, map[report.WarningKind]int{
		report.WarningUnknownKey:     1,
		report.WarningIgnoredValue:   1,
		report.WarningStrandedHeader: 1,
	}, kinds, "%v", warnings.List())
}

// TestWarningsNameTheLineOfTheFile: each warning names the line of the file
// it is about, counted from the opening delimiter, however many lines the
// front matter and the headers take out of the body.
func TestWarningsNameTheLineOfTheFile(t *testing.T) {
	document := "---\n" + // 1
		"title: Doc\n" + // 2
		"space: DOCS\n" + // 3
		"colour: blue\n" + // 4
		"labels:\n" + // 5
		"  - one\n" + // 6
		"  - 2\n" + // 7
		"---\n" + // 8
		"<!-- Parent: Home -->\n" + // 9
		"\n" + // 10
		"Text.\n" + // 11
		"\n" + // 12
		"```\n" + // 13
		"<!-- Label: example -->\n" + // 14, in code: not a warning
		"```\n" + // 15
		"\n" + // 16
		"<!-- Label: late -->\n" // 17

	warnings := report.NewWarnings("doc.md")
	_, _, err := ExtractMeta([]byte(document), ExtractOptions{
		Filename: "doc.md", FrontMatter: true, Warnings: warnings,
	})
	require.NoError(t, err)

	lines := map[report.WarningKind]int{}
	for _, warning := range warnings.List() {
		lines[warning.Kind] = warning.Line
	}

	assert.Equal(t, map[report.WarningKind]int{
		report.WarningUnknownKey:     4,
		report.WarningIgnoredValue:   5,
		report.WarningStrandedHeader: 17,
	}, lines, "%v", warnings.List())
}

// TestAStrandedHeaderInADocumentWithoutFrontMatterNamesItsLine: with headers
// only, the lines count from the top of the file.
func TestAStrandedHeaderInADocumentWithoutFrontMatterNamesItsLine(t *testing.T) {
	document := "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nText.\n\n<!-- Label: late -->\n"

	warnings := report.NewWarnings("doc.md")
	_, _, err := ExtractMeta([]byte(document), ExtractOptions{Filename: "doc.md", Warnings: warnings})
	require.NoError(t, err)

	list := warnings.List()
	require.Len(t, list, 1)
	assert.Equal(t, report.WarningStrandedHeader, list[0].Kind)
	assert.Equal(t, 6, list[0].Line)
}
