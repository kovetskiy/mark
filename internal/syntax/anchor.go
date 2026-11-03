package syntax

import (
	"strings"
	"unicode"
)

// AnchorKey reduces an id or a link target to what the two conventions -- the
// heading ids mark generates and the slugs authors write -- agree on: the
// letters and digits, in order, folded to lower case.
//
// Everything else is discarded rather than mapped, because the conventions do
// not merely punctuate differently -- they disagree about which characters
// survive at all. mark keeps "/" and "." in an id where a slug drops them, so
// "API/v2 Guide" becomes "API/v2-Guide" one way and "apiv2-guide" the other.
// Comparing only the alphanumerics is what makes those the same heading.
//
// "Letters and digits" means in every script, since mark keeps non-ASCII
// letters in an id.
func AnchorKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SplitPageAnchor separates a page title from the anchor written after it.
//
// The link checker looks up the title the renderer links to, so both split
// here.
//
// A "#" alone is not enough to split on: "ac:C# Guide" is a page whose title
// contains one, and reading it as a page called "C" with an anchor called
// " Guide" would break a link that works today. So the part after the last "#"
// has to look like an anchor -- present, and with no whitespace in it, which is
// true of every id mark generates, since a heading's spaces become hyphens.
//
// The last "#" rather than the first, so a title containing one can still be
// given an anchor.
func SplitPageAnchor(destination string) (title, anchor string) {
	at := strings.LastIndex(destination, "#")
	if at <= 0 || at == len(destination)-1 {
		return destination, ""
	}

	candidate := destination[at+1:]
	if strings.ContainsFunc(candidate, unicode.IsSpace) {
		return destination, ""
	}

	return destination[:at], candidate
}
