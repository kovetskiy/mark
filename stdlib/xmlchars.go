package stdlib

import (
	"errors"
	"html"
	"strconv"
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

// SanitizeMarkup replaces every character in a body of storage format that
// XML 1.0 cannot carry with U+FFFD, and reports how many it replaced.
//
// The stdlib escaping functions do this for what goes through them, but most
// of a page does not: goldmark writes text, code spans, link titles and table
// cells itself, and raw HTML is passed through as written. A form feed in a
// paragraph or an ANSI colour code in a code span therefore reached the page as
// it was, and Confluence refused the whole page for one invisible character.
// So does a numeric reference to one -- "&#27;" in raw HTML is written as it
// is, and in text goldmark resolves it to the ESC itself -- so a reference to a
// code point XML does not allow is replaced too. Only outside CDATA sections
// and comments, where a reference is not one but plain text.
func SanitizeMarkup(body string) (string, int) {
	body, replaced := ReplaceIllegalXMLChars(body)

	if !strings.Contains(body, "&#") {
		return body, replaced
	}

	var b strings.Builder
	b.Grow(len(body))

	rest := body
	for {
		at := strings.IndexAny(rest, "<&")
		if at == -1 {
			b.WriteString(rest)

			break
		}

		b.WriteString(rest[:at])
		rest = rest[at:]

		// A CDATA section or a comment is copied through to its end, since a
		// reference inside it is text rather than a character.
		if skip := skipLiteral(rest); skip > 0 {
			b.WriteString(rest[:skip])
			rest = rest[skip:]

			continue
		}

		if size, ok := illegalCharRef(rest); ok {
			b.WriteRune('�')
			rest = rest[size:]
			replaced++

			continue
		}

		b.WriteByte(rest[0])
		rest = rest[1:]
	}

	return b.String(), replaced
}

// skipLiteral returns the length of the CDATA section or comment s opens with,
// up to and including its end, or 0 when it opens with neither. An unterminated
// one runs to the end of s.
func skipLiteral(s string) int {
	for _, delimiters := range [][2]string{
		{"<![CDATA[", "]]>"},
		{"<!--", "-->"},
	} {
		if !strings.HasPrefix(s, delimiters[0]) {
			continue
		}

		end := strings.Index(s[len(delimiters[0]):], delimiters[1])
		if end == -1 {
			return len(s)
		}

		return len(delimiters[0]) + end + len(delimiters[1])
	}

	return 0
}

// illegalCharRef reports whether s opens with a numeric character reference to
// a code point XML 1.0 does not allow, and how long that reference is.
func illegalCharRef(s string) (int, bool) {
	if !strings.HasPrefix(s, "&#") {
		return 0, false
	}

	end := strings.IndexByte(s, ';')
	if end == -1 {
		return 0, false
	}

	digits, base := s[2:end], 10
	if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
		digits, base = digits[1:], 16
	}

	v, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		// Out of range is as illegal as any other code point XML has no
		// character for; anything else is not a number, so not a reference.
		return end + 1, errors.Is(err, strconv.ErrRange)
	}

	return end + 1, !IsXMLCodePoint(v)
}
