package parser

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type MathLatexInline struct {
	ast.BaseInline

	Equation []byte
}

func (n *MathLatexInline) Inline() {}

func (n *MathLatexInline) IsBlank(source []byte) bool {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		text := c.(*ast.Text).Segment
		if !util.IsBlank(text.Value(source)) {
			return false
		}
	}
	return true
}

func (n *MathLatexInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

var KindMathLatexInline = ast.NewNodeKind("MathLatexInline")

func (n *MathLatexInline) Kind() ast.NodeKind {
	return KindMathLatexInline
}

type MathLatexBlock struct {
	ast.BaseInline

	Equation []byte
}

func (n *MathLatexBlock) IsBlank(source []byte) bool {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		text := c.(*ast.Text).Segment
		if !util.IsBlank(text.Value(source)) {
			return false
		}
	}
	return true
}

func (n *MathLatexBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

var KindMathLatexBlock = ast.NewNodeKind("MathLatexBlock")

func (n *MathLatexBlock) Kind() ast.NodeKind {
	return KindMathLatexBlock
}

type MathLatexParser struct {
}

func NewMathLatexParser() parser.InlineParser {
	return &MathLatexParser{}
}

func (s *MathLatexParser) Trigger() []byte {
	return []byte{'$'}
}

func (s *MathLatexParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	buf := block.Source()
	ln, pos := block.Position()

	lstart := pos.Start
	lend := pos.Stop
	line := buf[lstart:lend]

	var start, end, advance int

	trigger := line[0]

	display := len(line) > 1 && line[1] == trigger

	if display { // Display
		start = lstart + 2

		offset := 2

	L:
		for x := 0; x < 20; x++ {
			for j := offset; j < len(line); j++ {
				if len(line) > j+1 && line[j] == trigger && line[j+1] == trigger {
					end = lstart + j
					advance = 2
					break L
				}
			}
			if lend == len(buf) {
				break
			}
			if end == 0 {
				rest := buf[lend:]
				j := 1
				for j < len(rest) && rest[j] != '\n' {
					j++
				}
				lstart = lend
				lend += j
				line = buf[lstart:lend]
				ln++
				offset = 0
			}
		}

	} else { // Inline
		start = lstart + 1

		for i := 1; i < len(line); i++ {
			c := line[i]
			if c == '\\' {
				i++
				continue
			}
			if c == trigger {
				end = lstart + i
				advance = 1
				break
			}
		}
		if end >= len(buf) || buf[end] != trigger {
			return nil
		}
	}

	if start >= end {
		return nil
	}

	newpos := end + advance
	if newpos < lend {
		block.SetPosition(ln, text.NewSegment(newpos, lend))
	} else {
		block.Advance(newpos)
	}

	if display {
		return &MathLatexBlock{
			Equation: buf[start:end],
		}
	} else {
		return &MathLatexInline{
			Equation: buf[start:end],
		}
	}
}
