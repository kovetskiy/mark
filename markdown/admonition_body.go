package markdown

import (
	"bytes"
	"strings"

	"github.com/kovetskiy/mark/v17/internal/mdsource"
	"github.com/kovetskiy/mark/v17/report"
)

// openingLineLimit is how much of an admonition's opening line a warning
// quotes.
const openingLineLimit = 60

// warnStrandedBodies tells the author of each admonition that is published
// without the lines written as its body: those that follow its opening line
// directly, are not indented under it, and are not ended by a closing line.
// The page shows them under the box and nothing on it says why, so the
// warning names the line and what keeps them in the box.
//
// compiled is the text that was parsed, which at holds offsets into, and
// written the text CompileMarkdown was given, whose lines the warnings name.
func warnStrandedBodies(written, compiled []byte, at []int, warnings *report.Warnings) {
	lineOf := mdsource.LinesOf(compiled, written)

	// The admonitions come in the order they were parsed, so each line is
	// counted on from the one before.
	counted, line := 0, 1

	for _, start := range at {
		if start < 0 || start >= len(compiled) {
			continue
		}

		if start < counted {
			counted, line = 0, 1
		}

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

		closing := string(opening[:run])
		warnings.AddAt(lineOf(line), report.WarningAdmonitionBody,
			"the lines after %q are not indented and no line of %q ends them, so they are published after "+
				"the admonition and not in it; indent them under it, or end them with a line of %q",
			shortened(strings.TrimSpace(string(opening)), openingLineLimit), closing, closing,
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
