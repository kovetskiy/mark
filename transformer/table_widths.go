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

var tableWidthsDirective = regexp.MustCompile(`(?is)^<!--\s*Table-Widths\s*:(.*?)-->$`)

// TableWidthsTransformer reads `<!-- Table-Widths: 160,720 -->` comments and
// gives the table that follows a <colgroup> with those widths.
//
// The comment is an HTML comment so that GitHub and GitLab still show a plain
// table; only Confluence gets the column widths.
type TableWidthsTransformer struct {
	// FilePath names the document in warnings. The source the transformer
	// sees has lost its header comments and had its macros and includes
	// expanded, so a line counted in it is not a line of the file; the
	// warnings quote the directive instead.
	FilePath string
}

// NewTableWidthsTransformer creates a new instance of TableWidthsTransformer
// for the document at path.
func NewTableWidthsTransformer(path string) *TableWidthsTransformer {
	return &TableWidthsTransformer{FilePath: path}
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
		// Blank lines leave no node behind, so "the next sibling" is what
		// "immediately before" means once Markdown formatters add them.
		table, ok := d.block.NextSibling().(*ext_ast.Table)
		if !ok {
			t.warn("Table-Widths %q is not directly followed by a table; ignoring it", d.value)

			continue
		}

		parent := d.block.Parent()
		parent.RemoveChild(parent, d.block)

		widths, err := parseTableWidths(d.value)
		if err != nil {
			t.warn("Table-Widths %q: %v; publishing the table without column widths", d.value, err)

			continue
		}

		if len(widths) != len(table.Alignments) {
			t.warn(
				"Table-Widths %q lists %d widths but the table has %d columns; publishing the table without column widths",
				d.value, len(widths), len(table.Alignments),
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

func (t *TableWidthsTransformer) warn(format string, args ...any) {
	log.Warn().Str("file", t.FilePath).Msgf(format, args...)
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
