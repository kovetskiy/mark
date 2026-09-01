package parser

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// MathBlock is a display formula written on lines of its own:
//
//	$$
//	\begin{aligned}
//	a &= b \\
//	c &= d
//	\end{aligned}
//	$$
//
// A block rather than an inline node because that is what it is. An inline
// parser is handed one block at a time, so a formula containing a blank line --
// which is how aligned environments are written -- is two blocks to goldmark
// and cannot be seen whole from inside either of them. Reading the source past
// the end of the block instead is what the inline parser used to do, and it
// swallowed the paragraphs in between and published them twice.
//
// The block form also settles two things the inline one gets wrong by
// construction. Its lines come from the block's own segments, so a formula
// inside a blockquote or a list item carries neither the "> " nor the
// indentation into the equation. And it cannot fire mid-sentence, so "\[" in
// running prose stays the escape for a literal bracket that CommonMark says it
// is.
type MathBlock struct {
	ast.BaseBlock

	// Equation is the formula's source, assembled once the closing fence is
	// found. Empty until then.
	Equation []byte
}

func (m *MathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(m, source, level, map[string]string{
		"Equation": string(m.Equation),
	}, nil)
}

var KindMathBlock = ast.NewNodeKind("MathBlock")

func (m *MathBlock) Kind() ast.NodeKind {
	return KindMathBlock
}

// IsRaw keeps goldmark from parsing inlines into the block.
//
// Without it the parser walks the block's lines and builds Text children out of
// them, exactly as it does for a paragraph -- so the formula was published as
// its picture and then again, immediately after it, as the TeX source. A fenced
// code block declares itself raw for the same reason.
func (m *MathBlock) IsRaw() bool {
	return true
}

func NewMathBlock() *MathBlock {
	return &MathBlock{}
}

// mathBlockFence is the marker that opens and closes the block. Only "$$" has
// the block form: "\[" and "\]" are written around a formula rather than on
// lines of their own, and a document that puts them on their own lines is read
// the same way by the inline parser.
var mathBlockFence = []byte("$$")

type mathBlockParser struct{}

// NewMathBlockParser returns a parser for display formulas fenced by "$$" on a
// line of its own.
func NewMathBlockParser() parser.BlockParser {
	return &mathBlockParser{}
}

func (b *mathBlockParser) Trigger() []byte {
	return []byte{'$'}
}

func (b *mathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()

	pos := pc.BlockOffset()
	if pos < 0 || !bytes.HasPrefix(line[pos:], mathBlockFence) {
		return nil, parser.NoChildren
	}

	// Only a fence with nothing after it opens a block. A formula that opens
	// and closes on one line -- "$$E = mc^2$$", whether alone on the line or in
	// the middle of a sentence -- is left to the inline parser, which has
	// always handled it and whose one-line answer never needed bounding.
	if len(bytes.TrimSpace(line[pos+len(mathBlockFence):])) > 0 {
		return nil, parser.NoChildren
	}

	reader.Advance(segment.Len() - 1)

	return NewMathBlock(), parser.NoChildren
}

func (b *mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, segment := reader.PeekLine()

	if bytes.Equal(bytes.TrimSpace(line), mathBlockFence) {
		reader.Advance(segment.Len() - 1)

		return parser.Close
	}

	node.Lines().Append(segment)

	return parser.Continue | parser.NoChildren
}

func (b *mathBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	block, ok := node.(*MathBlock)
	if !ok || len(block.Equation) > 0 {
		return
	}

	// Assembled from the block's own line segments, which is what leaves a
	// blockquote's "> " and a list item's indentation out of the equation:
	// goldmark has already taken the prefix off each line by the time it
	// records the segment.
	var buf bytes.Buffer

	lines := block.Lines()
	source := reader.Source()

	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(source))
	}

	block.Equation = bytes.TrimSpace(buf.Bytes())
}

func (b *mathBlockParser) CanInterruptParagraph() bool {
	return true
}

func (b *mathBlockParser) CanAcceptIndentedLine() bool {
	// Four spaces make an indented code block, and a formula indented that far
	// is a code sample about one.
	return false
}
