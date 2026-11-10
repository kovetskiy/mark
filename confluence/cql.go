package confluence

import (
	"strings"
	"unicode/utf8"
)

// QuoteCQL returns value as a double-quoted CQL string literal, for building
// a search such as
//
//	cql := "space = " + confluence.QuoteCQL(spaceKey) +
//		" AND label = " + confluence.QuoteCQL(label)
//
// A backslash becomes \\ and a double quote \", the only escapes CQL
// documents; everything else, non-ASCII included, is kept as written. Go's
// %q is not a substitute: it writes escapes such as \t, \u00a0 and \x00
// that CQL does not define.
//
// Tab, newline and carriage return are kept too, since CQL has no escape for
// them and a quoted string carries them. The other ASCII control characters,
// U+0000 to U+001F, and bytes that are not UTF-8 become U+FFFD: XML cannot
// hold them, so no Confluence title or name does, and the search matches
// nothing rather than being refused.
//
// The literal is exact for = and !=. On the right of ~ it is a text query,
// where Confluence's text-search syntax still applies inside the quotes:
// * and ? are wildcards, and a value wrapped in escaped quotes, such as
// QuoteCQL(`"hello dolly"`), is a phrase.
func QuoteCQL(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')

	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		i += size

		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(r)
		case r < 0x20 || (r == utf8.RuneError && size == 1):
			b.WriteRune(utf8.RuneError)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
