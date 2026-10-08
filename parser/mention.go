package parser

import (
	"bytes"

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

	index := bytes.IndexByte(line, '}')
	if index == -1 || index <= 2 {
		return nil
	}

	name := line[2:index]
	block.Advance(index + 1)

	return NewMention(name)
}
