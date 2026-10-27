package export

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark-emoji/v2/definition"
)

type inlineOptions struct {
	// raw is text between storage-format tags written out verbatim: it has
	// been XML-escaped already, and its entities are meant for Confluence.
	raw bool

	// table is text in a cell of a GFM table, where a pipe ends the cell.
	table bool
}

var entityPattern = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// escapeInline escapes text so that Markdown reads it back as the same text
// rather than as markup.
//
// It escapes what would otherwise start something -- emphasis, a code span, a
// link, an HTML tag or an entity, strikethrough, a mention, an autolink, and
// with their features a formula, an emoji or a date -- and
// leaves alone what cannot, so that "snake_case" stays readable. What only
// matters at the start of a line is escapeLineStarts' job.
func escapeInline(text string, opts inlineOptions) string {
	runes := []rune(text)

	var b strings.Builder
	b.Grow(len(text))

	for i, r := range runes {
		prev, next := rune(0), rune(0)
		if i > 0 {
			prev = runes[i-1]
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		switch r {
		case '\\', '*', '`', '[', ']', '~':
			b.WriteRune('\\')

		case '_':
			// Inside a word an underscore is never emphasis.
			if !isAlnum(prev) || !isAlnum(next) {
				b.WriteRune('\\')
			}

		case '<':
			if !opts.raw && (unicode.IsLetter(next) || next == '/' || next == '!' || next == '?') {
				b.WriteRune('\\')
			}

		case '&':
			if !opts.raw && entityPattern.MatchString(string(runes[i:min(i+40, len(runes))])) {
				b.WriteRune('\\')
			}

		case '@':
			// "@{name}" is a mention, "user@example.com" an address the
			// autolinker would make a link of, and "@date(...)" a date.
			if next == '{' || (isAlnum(prev) && isAlnum(next)) || isDate(runes[i:]) {
				b.WriteRune('\\')
			}

		case ':':
			// "https://example.com" in plain text would be linked, and
			// ":smile:" would be an emoji.
			if (next == '/' && i+2 < len(runes) && runes[i+2] == '/' && unicode.IsLetter(prev)) ||
				isEmoji(runes[i:]) {
				b.WriteRune('\\')
			}

		case '$':
			// The math feature opens a formula at a "$" followed by anything
			// but a space: another "$", or the formula itself.
			if next != ' ' && next != '\t' && next != '\n' {
				b.WriteRune('\\')
			}

		case '.':
			// As would "www.example.com".
			if i >= 3 && string(runes[i-3:i]) == "www" && (i == 3 || !isAlnum(runes[i-4])) {
				b.WriteRune('\\')
			}

		case '|':
			if opts.table || opts.raw {
				b.WriteRune('\\')
			}
		}

		b.WriteRune(r)
	}

	return b.String()
}

// dateMacro is what the date feature's parser takes for a date.
var dateMacro = regexp.MustCompile(`^@date\(([^)]+)\)`)

// isDate reports whether text starts with what the date feature publishes as
// a date: "@date(" and a value that is more than spaces and quotes.
func isDate(text []rune) bool {
	if len(text) < len("@date()") || string(text[:len("@date(")]) != "@date(" {
		return false
	}
	m := dateMacro.FindStringSubmatch(string(text))
	return m != nil && strings.Trim(strings.TrimSpace(m[1]), `"'`) != ""
}

var emojis = definition.Github()

// isEmoji reports whether text starts with what the emoji feature publishes
// as an emoji: a shortcode it knows, between colons.
func isEmoji(text []rune) bool {
	end := 1
	for end < len(text) && isShortcodeRune(text[end]) {
		end++
	}
	if end == 1 || end >= len(text) || text[end] != ':' {
		return false
	}
	_, ok := emojis.Get(string(text[1:end]))
	return ok
}

// isShortcodeRune is the emoji parser's test for a character of a shortcode.
func isShortcodeRune(r rune) bool {
	return r < unicode.MaxASCII && (isAlnum(r) || r == '_' || r == '-' || r == '+')
}

func isAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

var (
	lineHeading    = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)
	lineBullet     = regexp.MustCompile(`^[-+](?:[ \t]|$)`)
	lineRule       = regexp.MustCompile(`^(?:=+|-+|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})[ \t]*$`)
	lineOrdered    = regexp.MustCompile(`^([0-9]{1,9})([.)])(?:[ \t]|$)`)
	lineDefinition = regexp.MustCompile(`^:(?:[ \t]|$)`)
	lineAdmonition = regexp.MustCompile(`^!!!`)
	lineBlockquote = regexp.MustCompile(`^>`)
	// A GFM table's delimiter row, which makes the line before it a header
	// row and the paragraph a table: nothing but pipes, colons, dashes and
	// spaces.
	lineTableDelimiter = regexp.MustCompile(`^[ \t|:-]*-[ \t|:-]*$`)
	leadingSpace       = regexp.MustCompile(`^[ \t]+`)
	trailingSpace      = regexp.MustCompile(`[ \t]+$`)
	softBreakSpaces    = regexp.MustCompile(`[ \t]*\n[ \t]*`)
)

// escapeLineStarts escapes what would make a line of a paragraph into
// something else: a heading, a list item, a rule, a blockquote, a setext
// underline, a definition, a table's delimiter row.
func escapeLineStarts(paragraph string) string {
	lines := strings.Split(paragraph, "\n")
	for i, line := range lines {
		line = leadingSpace.ReplaceAllString(line, "")
		if !strings.HasSuffix(line, "\\") {
			// Two trailing spaces are a hard line break.
			line = trailingSpace.ReplaceAllString(line, "")
		}

		switch {
		case lineOrdered.MatchString(line):
			m := lineOrdered.FindStringSubmatchIndex(line)
			line = line[:m[4]] + "\\" + line[m[4]:]
		case lineHeading.MatchString(line), lineBullet.MatchString(line), lineRule.MatchString(line),
			lineDefinition.MatchString(line), lineAdmonition.MatchString(line), lineBlockquote.MatchString(line),
			lineTableDelimiter.MatchString(line):
			line = "\\" + line
		}

		lines[i] = line
	}

	return strings.Join(lines, "\n")
}
