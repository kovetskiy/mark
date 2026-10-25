package includes

import "strings"

// The scanners below find a <!-- Macro: --> directive's extent. They live here
// rather than in macro, which imports this package, because an included file
// has to protect its directives before macro.ExtractMacros ever sees them, and
// both have to agree on where a directive ends.

// NextMacroDirective is the offset of the first comment at or after from whose
// text begins with "Macro:", or -1.
func NextMacroDirective(s string, from int) int {
	for {
		idx := strings.Index(s[from:], "<!--")
		if idx == -1 {
			return -1
		}

		start := from + idx
		if strings.HasPrefix(strings.TrimLeft(s[start+4:], " \t\r\n"), "Macro:") {
			return start
		}

		from = start + 4
	}
}

// MacroDirectiveEnd is the offset just past the macro directive comment that
// starts at s[0]. The search stops at the next directive, if there is one.
//
// A macro whose pattern contains nested comment markers (for example
// "<!-- Macro: <!-- ac:details -->(...)<!-- ac:details end -->") has more than
// one "-->" inside the directive itself, so the terminator is found by matching
// nesting depth. Taking the last "-->" before the limit would swallow the
// document text the macro is meant to transform, since with a single macro the
// limit is the rest of the file. An unbalanced directive falls back to that
// last "-->", then to the first one anywhere, then to the end of s.
func MacroDirectiveEnd(s string) int {
	limit := len(s)
	if next := NextMacroDirective(s, 4); next != -1 {
		limit = next
	}

	end := directiveEnd(s[:limit])
	if end == -1 {
		end = strings.LastIndex(s[:limit], "-->")
	}
	if end == -1 {
		end = strings.Index(s, "-->")
	}
	if end == -1 {
		return len(s)
	}

	return end + len("-->")
}

// directiveEnd returns the index of the "-->" that closes the macro directive
// comment starting at s[0], accounting for "<!--" markers nested inside the
// directive's own pattern, or -1 when the comment is unterminated.
//
// The nesting counter is what distinguishes a nested pattern marker from the
// directive's real terminator: "<!-- Macro: <!-- ac:details -->" opens two
// comments and so needs two "-->" before the directive is closed.
func directiveEnd(s string) int {
	depth := 0
	for i := 0; i+1 < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			depth++
			i += 4
		case strings.HasPrefix(s[i:], "-->"):
			depth--
			if depth <= 0 {
				return i
			}
			i += 3
		default:
			i++
		}
	}
	return -1
}
