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
