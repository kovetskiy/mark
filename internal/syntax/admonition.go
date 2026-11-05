package syntax

import (
	"regexp"
	"strings"
)

// admonitionWords are the words that name an admonition in the colon syntax,
// with their plurals.
const admonitionWords = `info|infos|note|notes|warn|warns|warning|warnings|tip|tips`

// colonMarker matches a line that opens with an admonition word and a colon:
// "Info: text", "NOTES:", "Note:" alone. The colon is what tells a marker from
// a sentence that happens to start with the word, as "Note that" or "Tips are
// welcome" do. Emphasis is not in the text this sees, so "**Note:**" and
// "**Note**:" both arrive as "Note:".
var colonMarker = regexp.MustCompile(`^(?i)(?:` + admonitionWords + `):(?:\s|$)`)

// commentMarker matches the first line of an HTML comment that is a marker
// kept out of the rendered page: the word alone, with or without its colon.
var commentMarker = regexp.MustCompile(`^(?i)(?:` + admonitionWords + `):?$`)

// AdmonitionMarker returns the marker the first line of a blockquote opens
// with that makes mark publish it as an info, note, tip or warning macro, or
// "". The word is the marker up to its colon.
//
// A line that opens an HTML comment, "<!-- Info -->", is a marker when it
// holds nothing but the word: the comment is there only to choose the macro.
func AdmonitionMarker(literal string) string {
	line := strings.TrimSpace(literal)

	if inner, ok := strings.CutPrefix(line, "<!--"); ok {
		inner = strings.TrimSuffix(inner, "-->")
		return commentMarker.FindString(strings.TrimSpace(inner))
	}

	return colonMarker.FindString(line)
}

// AdmonitionWord returns the word of a marker AdmonitionMarker returned,
// lower-cased and without its colon or plural "s": "info", "note", "warn",
// "warning" or "tip".
func AdmonitionWord(marker string) string {
	word := strings.ToLower(strings.TrimSpace(marker))
	word = strings.TrimSuffix(word, ":")
	word = strings.TrimSpace(word)
	return strings.TrimSuffix(word, "s")
}
