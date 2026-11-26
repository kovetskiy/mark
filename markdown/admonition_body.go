package markdown

import (
	"bytes"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/internal/mdsource"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/yuin/goldmark/v2/ast"
)

// openingLineLimit is how much of an admonition's opening line a warning
// quotes.
const openingLineLimit = 60

// warnStrandedBodies tells the author of each admonition in doc that is
// published without the lines written as its body: those that follow its
// opening line directly, are not indented under it, and are not ended by a
// closing line. The page shows them under the box and nothing on it says
// why, so the warning names the line and what keeps them in the box.
//
// compiled is the text doc was parsed from, and written the text
// CompileMarkdown was given, whose lines the warnings name.
func warnStrandedBodies(written, compiled []byte, doc ast.Node, warnings *report.Warnings) {
	// In the order they were written, which a transformer can change, as it
	// moves a footnote's blocks to the end.
	var at []int

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if n, ok := node.(*cparser.Admonition); entering && ok && n.BodyFollows() {
			at = append(at, n.Pos())
		}

		return ast.WalkContinue, nil
	})
	slices.Sort(at)

	lineOf := mdsource.LinesOf(compiled, written)

	// Each line is counted on from the one before.
	counted, line := 0, 1

	for _, start := range at {
		line += bytes.Count(compiled[counted:start], []byte("\n"))
		counted = start

		opening := compiled[start:]
		if end := bytes.IndexByte(opening, '\n'); end >= 0 {
			opening = opening[:end]
		}

		run := 0
		for run < len(opening) && opening[run] == opening[0] {
			run++
		}

		quoted := shortened(strings.TrimSpace(string(opening)), openingLineLimit)

		// A '???' body has to be indented: no closing line takes level lines.
		if opening[0] == '?' {
			warnings.AddAt(lineOf(line), report.WarningAdmonitionBody,
				"the lines after %q are not indented, so they are published after the admonition and not in it; "+
					"indent them under it",
				quoted,
			)

			continue
		}

		closing := string(opening[:run])
		warnings.AddAt(lineOf(line), report.WarningAdmonitionBody,
			"the lines after %q are not indented and no line of %q ends them, so they are published after "+
				"the admonition and not in it; indent them under it, or end them with a line of %q",
			quoted, closing, closing,
		)
	}
}

// shortened returns s cut to limit characters, with an ellipsis for the rest.
func shortened(s string, limit int) string {
	if characters := []rune(s); len(characters) > limit {
		return string(characters[:limit]) + "..."
	}

	return s
}
