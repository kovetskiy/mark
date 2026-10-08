package transformer_test

import (
	"bytes"
	"testing"

	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func parseTableWidths(t *testing.T, source string) ast.Node {
	t.Helper()

	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(transformer.NewTableWidthsTransformer("test.md"), 100)),
		),
	)

	return md.Parser().Parse(text.NewReader([]byte(source)))
}

func firstTable(t *testing.T, doc ast.Node) *ext_ast.Table {
	t.Helper()

	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if table, ok := n.(*ext_ast.Table); ok {
			return table
		}
	}

	require.FailNow(t, "no table in document")

	return nil
}

// colgroup returns the verbatim markup the transformer put ahead of the
// table's header, or "" if it added none.
func colgroup(table *ext_ast.Table) string {
	if s, ok := table.FirstChild().(*ast.String); ok && s.IsCode() {
		return string(s.Value)
	}

	return ""
}

func TestTableWidthsTransformerSetsWidthsAndConsumesDirective(t *testing.T) {
	doc := parseTableWidths(t, "<!-- Table-Widths: 160, 720 -->\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")

	table := firstTable(t, doc)
	assert.Equal(t, "<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n", colgroup(table))
	assert.IsType(t, &ext_ast.Table{}, doc.FirstChild(), "the directive comment is gone")
}

func TestTableWidthsTransformerIsCaseInsensitive(t *testing.T) {
	table := firstTable(t, parseTableWidths(t, "<!--table-widths:5,6-->\n| a | b |\n|---|---|\n"))

	assert.NotEmpty(t, colgroup(table))
}

func TestTableWidthsTransformerLeavesColumnMismatchBare(t *testing.T) {
	table := firstTable(t, parseTableWidths(t, "<!-- Table-Widths: 1,2,3 -->\n| a | b |\n|---|---|\n"))

	assert.Empty(t, colgroup(table))
}

func TestTableWidthsTransformerIgnoresDirectiveSeparatedFromTable(t *testing.T) {
	doc := parseTableWidths(t, "<!-- Table-Widths: 1,2 -->\n\ntext\n\n| a | b |\n|---|---|\n")

	table := firstTable(t, doc)
	assert.Empty(t, colgroup(table))
	assert.IsType(t, &ast.HTMLBlock{}, doc.FirstChild(), "an unattached directive stays where it was written")
}

func TestTableWidthsTransformerColgroupRendersThroughGoldmark(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignStyle))),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(transformer.NewTableWidthsTransformer("test.md"), 100)),
		),
	)

	var out bytes.Buffer
	require.NoError(t, md.Convert([]byte("<!-- Table-Widths: 160,720 -->\n\n| a | b |\n| :-: | --: |\n| 1 | 2 |\n"), &out))

	assert.Contains(t, out.String(), "<table>\n<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n<thead>")
	assert.Contains(t, out.String(), `<th style="text-align:center">a</th>`)
	assert.Contains(t, out.String(), `<td style="text-align:right">2</td>`)
}

func TestTableWidthsTransformerAcceptsDirectiveOverSeveralLines(t *testing.T) {
	doc := parseTableWidths(t, "<!--\nTable-Widths:\n160,\n720\n-->\n\n| a | b |\n|---|---|\n")

	assert.Equal(t, "<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n", colgroup(firstTable(t, doc)))
	assert.IsType(t, &ext_ast.Table{}, doc.FirstChild(), "the directive comment is gone")
}
