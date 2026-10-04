package transformer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark/ast"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// TableWidthsAttribute names the node attribute that carries a table's column
// widths, in pixels, as an []int with one entry per column.
const TableWidthsAttribute = "mark-table-widths"

var tableWidthsDirective = regexp.MustCompile(`(?i)^<!--\s*Table-Widths\s*:(.*?)-->$`)

// TableWidthsTransformer reads `<!-- Table-Widths: 160,720 -->` comments and
// hands each width list to the table that follows.
//
// The comment is an HTML comment so that GitHub and GitLab still show a plain
// table; only Confluence gets the column widths.
type TableWidthsTransformer struct{}

// NewTableWidthsTransformer creates a new instance of TableWidthsTransformer.
func NewTableWidthsTransformer() *TableWidthsTransformer {
	return &TableWidthsTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *TableWidthsTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()

	var directives []*ast.HTMLBlock

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := node.(*ast.HTMLBlock); ok && entering {
			if _, matched := tableWidthsValue(block, source); matched {
				directives = append(directives, block)
			}
		}

		return ast.WalkContinue, nil
	})

	for _, block := range directives {
		value, _ := tableWidthsValue(block, source)
		line := getNodeLineNumber(block, source)

		// Blank lines leave no node behind, so "the next sibling" is what
		// "immediately before" means once Markdown formatters add them.
		table, ok := block.NextSibling().(*ext_ast.Table)
		if !ok {
			log.Warn().Msgf("line %d: Table-Widths is not directly followed by a table; ignoring it", line)

			continue
		}

		parent := block.Parent()
		parent.RemoveChild(parent, block)

		widths, err := parseTableWidths(value)
		if err != nil {
			log.Warn().Msgf("line %d: Table-Widths %q: %v; publishing the table without column widths", line, value, err)

			continue
		}

		if len(widths) != len(table.Alignments) {
			log.Warn().Msgf(
				"line %d: Table-Widths lists %d widths but the table has %d columns; publishing the table without column widths",
				line, len(widths), len(table.Alignments),
			)

			continue
		}

		table.SetAttribute([]byte(TableWidthsAttribute), widths)
	}
}

func tableWidthsValue(block *ast.HTMLBlock, source []byte) (string, bool) {
	raw := strings.TrimSpace(string(ExtractNodeRawContent(block, source)))

	match := tableWidthsDirective.FindStringSubmatch(raw)
	if match == nil {
		return "", false
	}

	return strings.TrimSpace(match[1]), true
}

// parseTableWidths accepts only plain positive pixel counts. Nothing else is
// ever interpolated into the output.
func parseTableWidths(value string) ([]int, error) {
	if value == "" {
		return nil, fmt.Errorf("no widths given")
	}

	parts := strings.Split(value, ",")
	widths := make([]int, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, fmt.Errorf("%q is not a width in pixels", part)
		}

		width, err := strconv.Atoi(part)
		if err != nil || width <= 0 {
			return nil, fmt.Errorf("%q is not a positive width in pixels", part)
		}

		widths = append(widths, width)
	}

	return widths, nil
}
