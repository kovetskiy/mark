package syntax

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// AttributeBlock reads the heading attribute block whose "{" is at
// source[start], in goldmark v1's grammar, the one heading attributes are
// written in:
//
//	block     = "{" { attribute [ "," ] } "}"
//	attribute = ( "#" | "." ) id-chars | name "=" value
//	value     = block | array | "\"" string "\"" | number | word
//
// A value is text when it is a string or a word other than true, false and
// null, and only text can be a class; a single quote opens no string; "=" needs
// a value; the block closes on its line. A string's backslash escapes are
// decoded and its entities are not, and a value that is not text is empty, as
// goldmark v1 rendered it. Several classes are joined with a space,
// where the first one stood. It
// returns the offset just past the closing "}".
func AttributeBlock(source []byte, start int) (end int, attrs []ast.Attribute, ok bool) {
	s := attributeScanner{source: source, pos: start}

	var classes []string

	class := -1

	add := func(name, value string) {
		if name == "class" {
			if class < 0 {
				class = len(attrs)
				attrs = append(attrs, ast.Attribute{Name: name})
			}

			classes = append(classes, value)

			return
		}

		attrs = append(attrs, ast.Attribute{
			Name:  name,
			Value: text.NewMultiLineValueFromString(value, text.IdentityDecoder),
		})
	}

	if !s.block(add) {
		return 0, nil, false
	}

	if class >= 0 {
		attrs[class].Value = text.NewMultiLineValueFromString(strings.Join(classes, " "), text.IdentityDecoder)
	}

	return s.pos, attrs, true
}

type attributeScanner struct {
	source []byte
	pos    int
}

func (s *attributeScanner) peek() byte {
	if s.pos >= len(s.source) || s.source[s.pos] == '\n' {
		return 0
	}

	return s.source[s.pos]
}

func (s *attributeScanner) skipSpaces() {
	for c := s.peek(); c == ' ' || c == '\t'; c = s.peek() {
		s.pos++
	}
}

// block scans a block at s.pos, handing each attribute to add when add is
// not nil.
func (s *attributeScanner) block(add func(name, value string)) bool {
	s.skipSpaces()

	if s.peek() != '{' {
		return false
	}

	s.pos++

	for {
		if s.peek() == '}' {
			s.pos++

			return true
		}

		name, value, ok := s.attribute()
		if !ok {
			return false
		}

		if add != nil {
			add(name, value)
		}

		s.skipSpaces()

		if s.peek() == ',' {
			s.pos++
			s.skipSpaces()
		}
	}
}

func (s *attributeScanner) attribute() (name, value string, ok bool) {
	s.skipSpaces()

	if c := s.peek(); c == '#' || c == '.' {
		s.pos++
		start := s.pos

		for c := s.peek(); c != 0 && !util.IsSpace(c) &&
			(!util.IsPunct(c) || c == '_' || c == '-' || c == ':' || c == '.'); c = s.peek() {
			s.pos++
		}

		name = "class"
		if c == '#' {
			name = "id"
		}

		return name, string(s.source[start:s.pos]), true
	}

	name, ok = s.word()
	if !ok {
		return "", "", false
	}

	s.skipSpaces()

	if s.peek() != '=' {
		return "", "", false
	}

	s.pos++
	s.skipSpaces()

	value, isText, ok := s.value()
	if !ok || (name == "class" && !isText) {
		return "", "", false
	}

	return name, value, true
}

// value scans a value, and returns it when it is text.
func (s *attributeScanner) value() (value string, isText, ok bool) {
	s.skipSpaces()

	switch c := s.peek(); {
	case c == 0:
		return "", false, false
	case c == '{':
		ok = s.block(nil)
	case c == '[':
		ok = s.array()
	case c == '"':
		value, ok = s.string()

		return value, true, ok
	case c == '-' || c == '+' || util.IsNumeric(c):
		ok = s.number()
	default:
		value, ok = s.word()
		if value == "true" || value == "false" || value == "null" {
			return "", false, ok
		}

		return value, ok, ok
	}

	return "", false, ok
}

func (s *attributeScanner) array() bool {
	s.pos++

	for i := 0; ; i++ {
		c := s.peek()

		comma := false
		if i != 0 && c == ',' {
			s.pos++
			comma = true
		}

		if c == ']' {
			if comma {
				return false
			}

			s.pos++

			return true
		}

		s.skipSpaces()

		if _, _, ok := s.value(); !ok {
			return false
		}

		s.skipSpaces()
	}
}

func (s *attributeScanner) string() (string, bool) {
	s.pos++

	var b strings.Builder

	for c := s.peek(); c != 0; c = s.peek() {
		if c == '"' {
			s.pos++

			return b.String(), true
		}

		if c == '\\' && s.pos+1 < len(s.source) && s.source[s.pos+1] != '\n' {
			if decoded, ok := stringEscapes[s.source[s.pos+1]]; ok {
				b.WriteByte(decoded)
				s.pos += 2

				continue
			}
		}

		b.WriteByte(c)
		s.pos++
	}

	return "", false
}

var stringEscapes = map[byte]byte{
	'"': '"', '/': '/', '\\': '\\',
	'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t',
}

func (s *attributeScanner) number() bool {
	start := s.pos

	if c := s.peek(); c == '-' || c == '+' {
		s.pos++
	}

	if !util.IsNumeric(s.peek()) {
		return false
	}

	s.digits()

	if s.peek() == '.' {
		s.pos++
		s.digits()
	}

	if c := s.peek(); c == 'e' || c == 'E' {
		s.pos++

		if c := s.peek(); c == '-' || c == '+' {
			s.pos++
		}

		s.digits()
	}

	_, err := strconv.ParseFloat(string(s.source[start:s.pos]), 64)

	return err == nil
}

func (s *attributeScanner) digits() {
	for util.IsNumeric(s.peek()) {
		s.pos++
	}
}

// word scans a name: a letter, "_" or ":", then letters, digits, "_", ":",
// "." and "-".
func (s *attributeScanner) word() (string, bool) {
	start := s.pos

	if !isNameStart(s.peek()) {
		return "", false
	}

	for c := s.peek(); isNameStart(c) || c >= '0' && c <= '9' || c == '.' || c == '-'; c = s.peek() {
		s.pos++
	}

	return string(s.source[start:s.pos]), true
}

func isNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == ':'
}
