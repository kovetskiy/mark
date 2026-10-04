package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

const tableSource = "| a | b |\n| :-: | --: |\n| 1 | 2 |\n"

func renderTable(t *testing.T, source string, custom bool) string {
	t.Helper()

	extensions := []goldmark.Extender{
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignStyle)),
	}
	opts := []parser.Option{
		parser.WithASTTransformers(util.Prioritized(transformer.NewTableWidthsTransformer(), 100)),
	}

	if !custom {
		return renderExtended(t, source, extensions, nil, opts...)
	}

	return renderExtended(t, source, extensions, []renderer.NodeRenderer{crenderer.NewConfluenceTableRenderer()}, opts...)
}

func TestTableRendererWithoutWidthsMatchesGoldmark(t *testing.T) {
	assert.Equal(t, renderTable(t, tableSource, false), renderTable(t, tableSource, true))
}

func TestTableRendererEmitsColgroupAfterTableOpens(t *testing.T) {
	out := renderTable(t, "<!-- Table-Widths: 160,720 -->\n\n"+tableSource, true)

	assert.Contains(t, out, "<table>\n<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n<thead>")
	assert.Contains(t, out, `<th style="text-align:center">a</th>`)
	assert.Contains(t, out, `<td style="text-align:right">2</td>`)
}
