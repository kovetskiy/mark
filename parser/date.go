package parser

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type DateNode struct {
	ast.BaseInline
	Value []byte
}

func (d *DateNode) Dump(source []byte, level int) {
	ast.DumpHelper(d, source, level, map[string]string{
		"Value": string(d.Value),
	}, nil)
}

var KindDate = ast.NewNodeKind("Date")

func (d *DateNode) Kind() ast.NodeKind {
	return KindDate
}

func NewDateNode(val []byte) *DateNode {
	return &DateNode{
		Value: val,
	}
}

type dateParser struct{}

func NewDateParser() parser.InlineParser {
	return &dateParser{}
}

func (s *dateParser) Trigger() []byte {
	return []byte{'@', '<'}
}

var (
	dateMacroRegex = regexp.MustCompile(`^@date\(([^)]+)\)`)
	timeTagRegex   = regexp.MustCompile(`(?i)^<time\b([^>]*)>(.*?)</time>|^<time\b([^>]*)/?>`)
	attrRegex      = regexp.MustCompile(`(?i)\bdatetime=["']?([^"' >]+)["']?`)
)

func (s *dateParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 {
		return nil
	}

	switch line[0] {
	case '@':
		loc := dateMacroRegex.FindSubmatchIndex(line)
		if loc != nil {
			matchLen := loc[1]
			valBytes := line[loc[2]:loc[3]]
			val := strings.TrimSpace(string(valBytes))
			val = strings.Trim(val, `"'`)
			if val != "" {
				block.Advance(matchLen)
				return NewDateNode([]byte(val))
			}
		}
	case '<':
		loc := timeTagRegex.FindSubmatchIndex(line)
		if loc != nil {
			matchLen := loc[1]
			var datetime string

			// Group 1: attrs in opening/closing tag, Group 2: inner text, Group 3: attrs in self-closing tag
			var attrsBytes, innerBytes []byte
			if loc[2] != -1 && loc[3] != -1 {
				attrsBytes = line[loc[2]:loc[3]]
			}
			if loc[4] != -1 && loc[5] != -1 {
				innerBytes = line[loc[4]:loc[5]]
			}
			if loc[6] != -1 && loc[7] != -1 {
				attrsBytes = line[loc[6]:loc[7]]
			}

			// Check for datetime attribute in attrs
			attrMatch := attrRegex.FindSubmatch(attrsBytes)
			if len(attrMatch) >= 2 {
				datetime = strings.TrimSpace(string(attrMatch[1]))
				datetime = strings.Trim(datetime, `"'`)
			} else if len(innerBytes) > 0 {
				datetime = strings.TrimSpace(string(innerBytes))
				datetime = strings.Trim(datetime, `"'`)
			}

			if datetime != "" {
				block.Advance(matchLen)
				return NewDateNode([]byte(datetime))
			}
		}
	}

	return nil
}
