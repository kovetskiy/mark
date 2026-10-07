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

var tableWidthsDirective = regexp.MustCompile(`(?i)^<!--\s*Table-Widths\s*:(.*?)-->$`)

// TableWidthsTransformer reads `<!-- Table-Widths: 160,720 -->` comments and
// gives the table that follows a <colgroup> with those widths.
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

	type directive struct {
		block *ast.HTMLBlock
		value string
	}

	var directives []directive

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := node.(*ast.HTMLBlock); ok && entering {
			if value, matched := tableWidthsValue(block, source); matched {
				directives = append(directives, directive{block, value})
			}
		}

		return ast.WalkContinue, nil
	})

	for _, d := range directives {
		block, value := d.block, d.value
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

		// goldmark's table renderer walks every child and only looks at its
		// own kinds, so a verbatim string ahead of the header comes out right
		// after <table>. SetCode, not SetRaw: a "raw" string is still escaped.
		colgroup := ast.NewString(colgroupMarkup(widths))
		colgroup.SetCode(true)
		table.InsertBefore(table, table.FirstChild(), colgroup)
	}
}

func colgroupMarkup(widths []int) []byte {
	var b strings.Builder

	b.WriteString("<colgroup>\n")
	for _, width := range widths {
		fmt.Fprintf(&b, "<col style=\"width: %dpx;\"/>\n", width)
	}
	b.WriteString("</colgroup>\n")

	return []byte(b.String())
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
