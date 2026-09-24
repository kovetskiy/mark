package mark

import (
	"errors"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/rs/zerolog/log"
)

// sanitizeXMLChars replaces every character in a compiled body that XML 1.0
// cannot carry with U+FFFD, and reports how many it replaced.
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
func sanitizeXMLChars(body string) (string, int) {
	body, replaced := stdlib.ReplaceIllegalXMLChars(body)

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

	return end + 1, !stdlib.IsXMLCodePoint(v)
}

// warnIllegalXMLChars tells the author, once per document, that characters
// were replaced -- whether by the pass over the compiled body or on the way
// through the stdlib escaping, which is why the source is looked at as well.
// Replacing them silently would leave a U+FFFD on the page with nothing to say
// where it came from.
func warnIllegalXMLChars(path string, source []byte, replaced int) {
	if replaced == 0 {
		if _, n := stdlib.ReplaceIllegalXMLChars(string(source)); n == 0 {
			return
		}
	}

	log.Warn().Msgf(
		"%s: replaced characters XML cannot carry, such as the control codes in "+
			"terminal output, with U+FFFD; Confluence rejects a page containing them",
		path,
	)
}
