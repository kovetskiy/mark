package transformer_test

import (
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
			parser.WithASTTransformers(util.Prioritized(transformer.NewTableWidthsTransformer(), 100)),
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

func TestTableWidthsTransformerSetsWidthsAndConsumesDirective(t *testing.T) {
	doc := parseTableWidths(t, "<!-- Table-Widths: 160, 720 -->\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")

	table := firstTable(t, doc)
	value, ok := table.AttributeString(transformer.TableWidthsAttribute)
	require.True(t, ok)
	assert.Equal(t, []int{160, 720}, value)
	assert.IsType(t, &ext_ast.Table{}, doc.FirstChild(), "the directive comment is gone")
}

func TestTableWidthsTransformerIsCaseInsensitive(t *testing.T) {
	table := firstTable(t, parseTableWidths(t, "<!--table-widths:5,6-->\n| a | b |\n|---|---|\n"))

	_, ok := table.AttributeString(transformer.TableWidthsAttribute)
	assert.True(t, ok)
}

func TestTableWidthsTransformerLeavesColumnMismatchBare(t *testing.T) {
	table := firstTable(t, parseTableWidths(t, "<!-- Table-Widths: 1,2,3 -->\n| a | b |\n|---|---|\n"))

	_, ok := table.AttributeString(transformer.TableWidthsAttribute)
	assert.False(t, ok)
}

func TestTableWidthsTransformerIgnoresDirectiveSeparatedFromTable(t *testing.T) {
	doc := parseTableWidths(t, "<!-- Table-Widths: 1,2 -->\n\ntext\n\n| a | b |\n|---|---|\n")

	table := firstTable(t, doc)
	_, ok := table.AttributeString(transformer.TableWidthsAttribute)
	assert.False(t, ok)
	assert.IsType(t, &ast.HTMLBlock{}, doc.FirstChild(), "an unattached directive stays where it was written")
}
