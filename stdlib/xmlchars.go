package stdlib

import (
	"html"
	"strings"
	"unicode/utf8"
)

// isXMLChar reports whether r is a character XML 1.0 allows in a document at
// all, escaped or not: tab, newline, carriage return and everything from space
// up, less the surrogates and U+FFFE and U+FFFF.
func isXMLChar(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r < 0x20:
		return false
	case r >= 0xD800 && r <= 0xDFFF:
		return false
	case r == 0xFFFE || r == 0xFFFF:
		return false
	default:
		return r <= utf8.MaxRune
	}
}

// IsXMLCodePoint reports whether a numeric character reference to v names a
// character XML 1.0 allows. "&#27;" is as fatal to a page as the ESC it names.
func IsXMLCodePoint(v uint64) bool {
	return v <= utf8.MaxRune && isXMLChar(rune(v))
}

// ReplaceIllegalXMLChars returns s with every character XML 1.0 cannot carry
// replaced by U+FFFD, and how many it replaced. Bytes that are not UTF-8 --
// among them a surrogate encoded on its own -- count as such a character.
//
// Escaping cannot help here: XML has no spelling for U+001B, not even "&#27;",
// so a single ANSI colour code in a pasted terminal log made Confluence refuse
// the whole page. The page is worth more than the control character.
func ReplaceIllegalXMLChars(s string) (string, int) {
	clean := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || !isXMLChar(r) {
			clean = false

			break
		}
		i += size
	}

	if clean {
		return s, 0
	}

	var b strings.Builder
	b.Grow(len(s))

	replaced := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || !isXMLChar(r) {
			b.WriteRune(utf8.RuneError)
			replaced++
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}

	return b.String(), replaced
}

// xmlChars is ReplaceIllegalXMLChars for a caller with no use for the count.
func xmlChars(s string) string {
	out, _ := ReplaceIllegalXMLChars(s)

	return out
}

// XMLEscape escapes s for XML text or an attribute value, and replaces the
// characters no escaping can make legal. It is what the xmlesc template
// function does, for Go code that builds markup itself.
func XMLEscape(s string) string {
	return html.EscapeString(xmlChars(s))
}

// CDATA prepares s for the inside of a CDATA section: an embedded "]]>" is
// split across two sections, the only way to write it in one, and the
// characters no section may hold are replaced. It is what the cdata template
// function does, for Go code that builds markup itself.
func CDATA(s string) string {
	return strings.ReplaceAll(xmlChars(s), "]]>", "]]><![CDATA[]]]]><![CDATA[>")
}
