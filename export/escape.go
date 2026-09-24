package export

import (
	"regexp"
	"strings"
	"unicode"
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
// link, an HTML tag or an entity, strikethrough, a mention, an autolink -- and
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
			// "@{name}" is a mention, and "user@example.com" an address the
			// autolinker would make a link of.
			if next == '{' || (isAlnum(prev) && isAlnum(next)) {
				b.WriteRune('\\')
			}

		case ':':
			// "https://example.com" in plain text would be linked.
			if next == '/' && i+2 < len(runes) && runes[i+2] == '/' && unicode.IsLetter(prev) {
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

func isAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

var (
	lineHeading     = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)
	lineBullet      = regexp.MustCompile(`^[-+](?:[ \t]|$)`)
	lineRule        = regexp.MustCompile(`^(?:=+|-+|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})[ \t]*$`)
	lineOrdered     = regexp.MustCompile(`^([0-9]{1,9})([.)])(?:[ \t]|$)`)
	lineDefinition  = regexp.MustCompile(`^:(?:[ \t]|$)`)
	lineAdmonition  = regexp.MustCompile(`^!!!`)
	lineBlockquote  = regexp.MustCompile(`^>`)
	leadingSpace    = regexp.MustCompile(`^[ \t]+`)
	trailingSpace   = regexp.MustCompile(`[ \t]+$`)
	softBreakSpaces = regexp.MustCompile(`[ \t]*\n[ \t]*`)
)

// escapeLineStarts escapes what would make a line of a paragraph into
// something else: a heading, a list item, a rule, a blockquote, a setext
// underline, a definition.
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
			lineDefinition.MatchString(line), lineAdmonition.MatchString(line), lineBlockquote.MatchString(line):
			line = "\\" + line
		}

		lines[i] = line
	}

	return strings.Join(lines, "\n")
}
