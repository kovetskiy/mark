package syntax

import (
	"regexp"
	"strings"
)

// admonitionWords are the words that name an admonition in the colon syntax.
// Each may also be written as a plural.
const admonitionWords = `(info|note|warn|warning|tip)s?`

// colonMarker matches a line that opens with an admonition word and a colon:
// "Info: text", "NOTES:", "Note:" alone. The colon is what tells a marker from
// a sentence that happens to start with the word, as "Note that" or "Tips are
// welcome" do.
var colonMarker = regexp.MustCompile(`^(?i)` + admonitionWords + `:(?:\s|$)`)

// commentMarker matches the inside of an HTML comment that holds nothing but
// the word, with or without its colon: the comment is there only to choose
// the macro.
var commentMarker = regexp.MustCompile(`^(?i)` + admonitionWords + `:?$`)

// AdmonitionWord returns the admonition word the first line of a blockquote
// opens with in the colon syntax, "> Info: text" or "> <!-- Info -->", or "".
// The word is lower-cased and singular: "info", "note", "warn", "warning" or
// "tip". Emphasis is not in the line it is given, so "**Note:**" and
// "**Note**:" both arrive as "Note:".
func AdmonitionWord(line string) string {
	line = strings.TrimSpace(line)

	marker := colonMarker
	if inner, ok := strings.CutPrefix(line, "<!--"); ok {
		line = strings.TrimSpace(strings.TrimSuffix(inner, "-->"))
		marker = commentMarker
	}

	if m := marker.FindStringSubmatch(line); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}
