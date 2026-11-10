package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

type Mention struct {
	ast.BaseInline
	Name []byte
}

func (m *Mention) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(m, map[string]any{
		"Name": string(m.Name),
	})
}

var KindMention = ast.NewNodeKind("Mention")

func (m *Mention) Kind() ast.NodeKind {
	return KindMention
}

func NewMention(name []byte) *Mention {
	m := &Mention{
		Name: name,
	}
	m.Init(m)
	return m
}

type mentionParser struct {
}

func NewMentionParser() parser.InlineParser {
	return &mentionParser{}
}

func (s *mentionParser) Trigger() []byte {
	return []byte{'@'}
}

func (s *mentionParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 3 || line[1] != '{' {
		return nil
	}

	name, length, ok := scanMentionName(line[2:])
	if !ok || length == 0 {
		return nil
	}

	block.Advance(2 + length + 1)

	return NewMention(name)
}

// mentionEscapes are the characters a backslash escapes inside "@{...}": the
// closing brace, the backslash itself, and the pipe, which has to be written
// \| inside a table cell and reaches this parser still escaped. A backslash
// before anything else is itself, so "@{DOMAIN\user}" names DOMAIN\user.
const mentionEscapes = `}\|`

// scanMentionName reads a mention's name up to its closing brace, with the
// escapes taken out. length is how much of s the name takes, without the
// brace, and ok is false when the brace never comes.
func scanMentionName(s []byte) (name []byte, length int, ok bool) {
	// Only allocated at the first escape: most names have none, and are the
	// source as it stands.
	var decoded []byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c == '}':
			if decoded == nil {
				return s[:i], i, true
			}
			return decoded, i, true

		case c == '\\' && i+1 < len(s) && strings.IndexByte(mentionEscapes, s[i+1]) >= 0:
			if decoded == nil {
				decoded = append([]byte{}, s[:i]...)
			}
			i++
			decoded = append(decoded, s[i])

			continue
		}

		if decoded != nil {
			decoded = append(decoded, c)
		}
	}

	return nil, 0, false
}

// ErrUnrepresentable is returned by FormatMention for a name no mention can
// hold.
var ErrUnrepresentable = errors.New("cannot be written as a mention")

// FormatMention returns the mention of the user called name, "@{name}", with
// the characters that would end it early escaped, so that mark mentions
// exactly name.
//
// It is for a program that writes mark documents from text it does not
// control. The mention reads back as name in a paragraph and in a table cell
// alike. A mention is one line and never empty, so a name that is empty or
// holds a line break is ErrUnrepresentable rather than altered.
//
// The text around the mention is the caller's to escape: a mention written
// inside a code span is code, as it is anywhere else.
func FormatMention(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("an empty name %w", ErrUnrepresentable)
	}
	if strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("name %q holds a line break and %w", name, ErrUnrepresentable)
	}

	var b strings.Builder
	b.WriteString("@{")
	for i := 0; i < len(name); i++ {
		if strings.IndexByte(mentionEscapes, name[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(name[i])
	}
	b.WriteByte('}')

	return b.String(), nil
}
