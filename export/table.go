package export

import (
	"regexp"
	"strings"
)

// table writes a table as a GFM table when it is one, and as HTML otherwise.
//
// A GFM table has a header row and then rows of the same number of cells, and a
// cell holds a single line of inline content. A table Confluence's editor made
// usually is one, give or take the <p> it puts in each cell. One with merged
// cells, no header, or a list or a macro in a cell is not, and is kept as it
// was: an HTML block, which mark publishes as written.
func (c *converter) table(n *node) string {
	rows, ok := tableRows(n)
	if !ok {
		return c.htmlBlock(n)
	}

	header := rows[0]
	for _, cell := range header {
		if !cell.is("th") {
			return c.htmlBlock(n)
		}
	}

	width := len(header)
	for _, row := range rows[1:] {
		if len(row) != width {
			return c.htmlBlock(n)
		}
		for _, cell := range row {
			if !cell.is("td") {
				return c.htmlBlock(n)
			}
		}
	}

	rendered := make([][]string, len(rows))
	for i, row := range rows {
		for _, cell := range row {
			text, ok := c.tableCell(cell)
			if !ok {
				return c.htmlBlock(n)
			}
			rendered[i] = append(rendered[i], text)
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("|")
		for _, cell := range cells {
			b.WriteString(" " + cell + " |")
		}
		b.WriteString("\n")
	}

	writeRow(rendered[0])

	delimiters := make([]string, width)
	for i, cell := range header {
		switch alignment(cell) {
		case "left":
			delimiters[i] = ":---"
		case "center":
			delimiters[i] = ":---:"
		case "right":
			delimiters[i] = "---:"
		default:
			delimiters[i] = "---"
		}
	}
	writeRow(delimiters)

	for _, row := range rendered[1:] {
		writeRow(row)
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// tableRows collects the cells of a table row by row, and says whether the
// table has nothing a GFM table cannot say: a caption, a merged cell, a row
// with nothing in it.
func tableRows(table *node) ([][]*node, bool) {
	var rows [][]*node

	var walk func(n *node) bool
	walk = func(n *node) bool {
		for _, child := range n.children {
			switch {
			case child.isBlank():
			case child.is("colgroup"):
			case child.is("thead", "tbody", "tfoot"):
				if !walk(child) {
					return false
				}
			case child.is("tr"):
				var cells []*node
				for _, cell := range child.children {
					switch {
					case cell.isBlank():
					case cell.is("th", "td"):
						if span := cell.attr("colspan"); span != "" && span != "1" {
							return false
						}
						if span := cell.attr("rowspan"); span != "" && span != "1" {
							return false
						}
						cells = append(cells, cell)
					default:
						return false
					}
				}
				if len(cells) == 0 {
					return false
				}
				rows = append(rows, cells)
			default:
				return false
			}
		}

		return true
	}

	if !walk(table) || len(rows) == 0 {
		return nil, false
	}

	return rows, true
}

// tableCell renders a cell's content on one line, if it is inline content or
// a single paragraph of it.
func (c *converter) tableCell(cell *node) (string, bool) {
	content := significant(cell.children)
	if len(content) == 1 && content[0].is("p") && len(content[0].attrs) == 0 {
		content = content[0].children
	}

	for _, n := range content {
		if isBlock(n) {
			return "", false
		}
	}

	w := c.inline(inlineContext{singleLine: true, table: true})
	w.nodes(content)
	text := strings.TrimSpace(w.finish())

	// A pipe ends the cell wherever it is, an attribute of markup kept as it
	// was included, and only text can have it escaped.
	if unescapedPipe.MatchString(text) {
		return "", false
	}

	return text, true
}

var unescapedPipe = regexp.MustCompile(`(^|[^\\])\|`)

var textAlign = regexp.MustCompile(`text-align:\s*(left|center|right)`)

func alignment(cell *node) string {
	if m := textAlign.FindStringSubmatch(cell.attr("style")); m != nil {
		return m[1]
	}

	return ""
}
