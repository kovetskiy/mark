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

	// fence is the pair of markers this block was opened with, so the closer
	// looked for is the one that matches the opener.
	fence fence
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

// fence is one pair of markers that can stand on lines of their own around a
// display formula.
type fence struct {
	// dollars says the opener is a run of two or more "$", closed by another
	// run of two or more. Counting rather than matching a fixed "$$" is what
	// makes "$$$" a fence rather than a "$$" with a stray dollar after it.
	dollars bool
	open    []byte
	close   []byte
}

var fences = []fence{
	{dollars: true},
	{open: []byte(`\[`), close: []byte(`\]`)},
}

// openingFence reports which fence, if any, a line opens with, and whether
// anything follows it.
//
// Only a fence with nothing after it opens a block. A formula that opens and
// closes on one line is left to the inline parser, which has always handled it
// and whose one-line answer never needed bounding.
func openingFence(line []byte, pos int) (fence, bool) {
	rest := line[pos:]

	if run := dollarRun(rest); run >= 2 {
		if len(bytes.TrimSpace(rest[run:])) > 0 {
			return fence{}, false
		}

		return fences[0], true
	}

	for _, f := range fences[1:] {
		if !bytes.HasPrefix(rest, f.open) {
			continue
		}
		if len(bytes.TrimSpace(rest[len(f.open):])) > 0 {
			return fence{}, false
		}

		return f, true
	}

	return fence{}, false
}

// closesBlock reports whether a line is the closing fence.
func (f fence) closesBlock(line []byte) bool {
	trimmed := bytes.TrimSpace(line)

	if f.dollars {
		return len(trimmed) >= 2 && dollarRun(trimmed) == len(trimmed)
	}

	return bytes.Equal(trimmed, f.close)
}

// dollarRun counts the "$" a slice opens with.
func dollarRun(b []byte) int {
	run := 0
	for run < len(b) && b[run] == '$' {
		run++
	}

	return run
}

type mathBlockParser struct{}

// NewMathBlockParser returns a parser for display formulas fenced by "$$" on a
// line of its own.
func NewMathBlockParser() parser.BlockParser {
	return &mathBlockParser{}
}

func (b *mathBlockParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (b *mathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()

	pos := pc.BlockOffset()
	if pos < 0 {
		return nil, parser.NoChildren
	}

	opened, ok := openingFence(line, pos)
	if !ok {
		return nil, parser.NoChildren
	}

	reader.Advance(segment.Len() - 1)

	node := NewMathBlock()
	node.fence = opened

	return node, parser.NoChildren
}

func (b *mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, segment := reader.PeekLine()

	block, ok := node.(*MathBlock)
	if ok && block.fence.closesBlock(line) {
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
