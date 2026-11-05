package syntax

import "strings"

// AdmonitionWords are the words that name an admonition in the colon syntax,
// lower-cased. Each may also be written as a plural, with an "s": "Notes:".
// MkDocs types such as "error" and "success" are not markers: too many quoted
// log lines open with them, "> Error: connection refused".
var AdmonitionWords = []string{
	"note", "info", "important",
	"tip", "hint",
	"warning", "warn", "attention",
	"caution", "danger",
}

// AdmonitionWord returns the admonition word the first line of a blockquote
// opens with in the colon syntax, "> Info: text" or "> <!-- Info -->", or "".
// The word is one of AdmonitionWords: lower-cased and singular. Emphasis is
// not in the line it is given, so "**Note:**" and "**Note**:" both arrive as
// "Note:".
//
// A marker is the word and a colon followed by a blank or the end of the line:
// the colon is what tells a marker from a sentence that happens to start with
// the word, as "Note that" or "Tips are welcome" do. In a comment the word is
// all there is, with or without its colon: the comment is there only to
// choose the macro.
func AdmonitionWord(line string) string {
	line = strings.TrimSpace(line)

	inner, comment := strings.CutPrefix(line, "<!--")
	if comment {
		line = strings.TrimSpace(strings.TrimSuffix(inner, "-->"))
	}

	end := 0
	for end < len(line) && isASCIILetter(line[end]) {
		end++
	}
	word, rest := line[:end], line[end:]

	if comment {
		if rest != "" && rest != ":" {
			return ""
		}
	} else {
		after, ok := strings.CutPrefix(rest, ":")
		if !ok || after != "" && !isMarkerBlank(after[0]) {
			return ""
		}
	}

	return admonitionWord(word)
}

// admonitionWord returns the word of AdmonitionWords that word spells, in any
// case, singular or plural, or "".
func admonitionWord(word string) string {
	singular := word
	if n := len(word) - 1; n > 0 && (word[n] == 's' || word[n] == 'S') {
		singular = word[:n]
	}
	for _, w := range AdmonitionWords {
		if strings.EqualFold(word, w) || strings.EqualFold(singular, w) {
			return w
		}
	}
	return ""
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// isMarkerBlank reports whether c may follow a marker's colon: a space, a tab
// or a line break.
func isMarkerBlank(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	}
	return false
}
