package renderer_test

import (
	"bytes"
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// renderDefinitionList compiles with the definition-list extension and mark's
// renderer for it, which is the pair every test here needs.
func renderDefinitionList(t *testing.T, input string) string {
	t.Helper()

	gm := goldmark.New(
		goldmark.WithExtensions(extension.DefinitionList),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(
				util.Prioritized(crenderer.NewConfluenceDefinitionListRenderer(), 100),
			),
		),
	)

	var buf bytes.Buffer
	require.NoError(t, gm.Convert([]byte(input), &buf))

	return buf.String()
}

// TestDefinitionListIsATable: storage format has no <dl>, <dt> or <dd>, so a
// definition list arrived as an unstructured run of text with its terms no
// longer distinguishable from what defines them. A two-column table is what the
// Confluence editor produces for this shape and what the elements mean.
func TestDefinitionListIsATable(t *testing.T) {
	actual := renderDefinitionList(t, "Term\n: Definition\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<th>Term</th>")
	assert.Contains(t, actual, "<td>Definition</td>")
	assert.Contains(t, actual, "<table>")

	for _, gone := range []string{"<dl>", "<dt>", "<dd>"} {
		assert.NotContains(t, actual, gone)
	}
}

// TestDefinitionListRowPerTerm: each term heads its own row.
func TestDefinitionListRowPerTerm(t *testing.T) {
	actual := renderDefinitionList(t, "One\n: First\n\nTwo\n: Second\n")
	assertWellFormed(t, actual)

	assert.Equal(t, 2, strings.Count(actual, "<tr>"))
	assert.Equal(t, 1, strings.Count(actual, "<table>"))
}

// TestSeveralDefinitionsShareACell: a term may be defined more than once, and
// giving each definition a row of its own would leave rows with no term to head
// them. Without a separator the two definitions run together as one word.
func TestSeveralDefinitionsShareACell(t *testing.T) {
	actual := renderDefinitionList(t, "Term\n: First\n: Second\n")
	assertWellFormed(t, actual)

	assert.Equal(t, 1, strings.Count(actual, "<tr>"))
	assert.Contains(t, actual, "First<br/>Second")
}

// TestDefinitionListKeepsInlineMarkup: the cells hold ordinary inline content.
func TestDefinitionListKeepsInlineMarkup(t *testing.T) {
	actual := renderDefinitionList(t, "**Bold** term\n: A `code` definition\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<strong>Bold</strong>")
	assert.Contains(t, actual, "<code>code</code>")
}

// TestNestedDefinitionListClosesBothRows: a definition may hold a definition
// list of its own, and the two lists' rows have nothing to do with each other.
// With one pair of flags shared between them the inner list reset the outer
// list's row on the way in and closed it on the way out, leaving a <td> and a
// <tr> that nothing closed -- and one such list anywhere in a document made the
// whole page fail the well-formedness check rather than only that list.
func TestNestedDefinitionListClosesBothRows(t *testing.T) {
	actual := renderDefinitionList(t,
		"Term A\n:   Def A\n\n    Inner\n    :   InnerDef\n\nTerm B\n:   Def B\n",
	)
	assertWellFormed(t, actual)

	assert.Equal(t, 2, strings.Count(actual, "<table>"), "one table per list")
	assert.Equal(t, 2, strings.Count(actual, "</table>"))

	// Three rows across the two tables, every one of them closed.
	assert.Equal(t, 3, strings.Count(actual, "<tr>"))
	assert.Equal(t, 3, strings.Count(actual, "</tr>"))
	assert.Equal(t, 3, strings.Count(actual, "</td>"))

	assert.Contains(t, actual, "<th>Inner</th>")
	assert.Contains(t, actual, "<th>Term B</th>")
}

// TestNestedDefinitionListLeavesTheOuterCellOpen: the inner table belongs
// inside the outer definition's cell, not beside it.
func TestNestedDefinitionListLeavesTheOuterCellOpen(t *testing.T) {
	actual := renderDefinitionList(t,
		"Outer\n:   Before\n\n    Inner\n    :   InnerDef\n",
	)
	assertWellFormed(t, actual)

	outer := strings.Index(actual, "<td>Before")
	inner := strings.Index(actual, "<th>Inner</th>")
	closes := strings.LastIndex(actual, "</td>")

	require.NotEqual(t, -1, outer)
	require.NotEqual(t, -1, inner)
	assert.Less(t, outer, inner, "the inner list sits inside the outer cell")
	assert.Less(t, inner, closes, "the outer cell closes after it")
}
