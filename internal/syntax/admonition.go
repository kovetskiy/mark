package syntax

import (
	"regexp"
	"strings"
)

// blockQuoteMarker matches a line that opens with an admonition marker.
//
// Anchored, and ending on a word boundary, so that "Information about
// pricing" or "Please note that" is not read as a marker.
//
// Everything the documented syntax allows still matches -- "Info: text",
// "**Note:** text", "NOTES:", a bare "Warn" -- because the marker is written
// at the front of the line in every one of them. Emphasis is not in the text
// this sees, goldmark having already made a node of it, so "**Warn**" arrives
// as "Warn".
var blockQuoteMarker = regexp.MustCompile(
	`^(?i)(info|infos|note|notes|warn|warns|warning|warnings|tip|tips)\b[[:punct:]]*(\s|$)`,
)

// AdmonitionMarker returns the word a blockquote's first line opens with that
// makes mark publish it as an info, note, tip or warning macro, or "".
//
// The comment form is stripped first: "<!-- Info -->" is a marker written in a
// way that keeps it out of the rendered page.
func AdmonitionMarker(literal string) string {
	marker := strings.TrimSpace(literal)
	marker = strings.TrimPrefix(marker, "<!--")
	marker = strings.TrimSuffix(marker, "-->")
	marker = strings.TrimSpace(marker)

	return blockQuoteMarker.FindString(marker)
}
