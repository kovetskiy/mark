package parser

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Math is one LaTeX formula, held as its source until a renderer decides what
// to do with it.
//
// Display is TeX's own distinction: an inline formula is set compactly in the
// run of the sentence, a display one is set larger and given room.
type Math struct {
	ast.BaseInline
	Equation []byte
	Display  bool
}

func (m *Math) Dump(source []byte, level int) {
	ast.DumpHelper(m, source, level, map[string]string{
		"Equation": string(m.Equation),
		"Display":  boolText(m.Display),
	}, nil)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var KindMath = ast.NewNodeKind("Math")

func (m *Math) Kind() ast.NodeKind {
	return KindMath
}

func NewMath(equation []byte, display bool) *Math {
	return &Math{
		Equation: equation,
		Display:  display,
	}
}

// delimiter is one pair of markers a formula can be written between.
type delimiter struct {
	open  string
	close string
	// display selects TeX's display mode.
	display bool
	// multiline says whether the formula may run past the end of the line.
	// Display formulas usually do; an inline one that appears to is far more
	// likely to be a stray dollar sign in prose.
	multiline bool
	// strict applies the rule that keeps prices out of the parser: no space
	// just inside either marker. It is only needed for "$", which is the only
	// marker that occurs in ordinary writing.
	strict bool
}

// delimiters are tried in order, so the two-character "$$" has to be tried
// before the one-character "$" that is its prefix.
var delimiters = []delimiter{
	{open: "$$", close: "$$", display: true, multiline: true},
	{open: `\[`, close: `\]`, display: true, multiline: true},
	{open: `\(`, close: `\)`},
	{open: "$", close: "$", strict: true},
}

type mathParser struct {
}

// NewMathParser returns a parser for LaTeX formulas written between the
// delimiters TeX and every other Markdown tool use.
func NewMathParser() parser.InlineParser {
	return &mathParser{}
}

func (s *mathParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (s *mathParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	source := block.Source()
	_, segment := block.Position()
	start := segment.Start

	// A formula ends inside the block it began in. Handing scan the whole
	// document let a "$$" written in one paragraph close against the next
	// "$$" three paragraphs further down: everything between them became the
	// equation, and since those paragraphs are still blocks of their own they
	// were published a second time as prose. Swallowed text carrying a "#" or
	// an "&" was worse -- LaTeX rejects both outside a macro, so the file
	// failed to compile at all.
	source = source[:blockEnd(parent, source)]

	for _, d := range delimiters {
		if start >= len(source) || !bytes.HasPrefix(source[start:], []byte(d.open)) {
			continue
		}

		equation, width, ok := d.scan(source, start)
		if !ok {
			// A "$" that opens nothing is a dollar sign, and a backslash that
			// opens nothing is an escape for goldmark to deal with. Trying the
			// remaining delimiters would not help -- they do not share a first
			// character with the one that just failed -- but it costs nothing
			// and keeps the loop honest.
			continue
		}

		block.Advance(width)

		return NewMath(equation, d.display)
	}

	return nil
}

// blockEnd is the offset just past the block goldmark is parsing inlines for.
//
// An inline parser is handed the source of the whole document; parent is the
// block node that owns every inline in this pass, so its own line segments are
// the only bound available. A block with no lines -- and the inline node an
// inline parser is never given as a parent, whose Lines panics -- leaves the
// source as it was.
func blockEnd(parent ast.Node, source []byte) int {
	if parent.Type() != ast.TypeBlock {
		return len(source)
	}

	lines := parent.Lines()
	if lines == nil || lines.Len() == 0 {
		return len(source)
	}

	last := lines.At(lines.Len() - 1)
	if last.Stop < 0 || last.Stop > len(source) {
		return len(source)
	}

	return last.Stop
}

// scan finds the end of a formula that starts at the opening marker, and
// returns the formula itself and how much of the source it occupied.
func (d delimiter) scan(source []byte, start int) (equation []byte, width int, ok bool) {
	from := start + len(d.open)
	if from >= len(source) {
		return nil, 0, false
	}

	rest := source[from:]
	if !d.multiline {
		if line := bytes.IndexByte(rest, '\n'); line >= 0 {
			rest = rest[:line]
		}
	}

	end := bytes.Index(rest, []byte(d.close))
	if end < 0 {
		return nil, 0, false
	}

	equation = rest[:end]
	if len(equation) == 0 {
		return nil, 0, false
	}

	// Without this, "it costs $5 and $7 today" is a formula: the parser finds
	// an opening dollar, a closing dollar, and text in between. Requiring the
	// formula to begin and end with something other than a space is the rule
	// every other Markdown implementation settled on, and it costs nothing that
	// a formula would want -- TeX ignores the spaces there anyway.
	if d.strict && (util.IsSpace(equation[0]) || util.IsSpace(equation[len(equation)-1])) {
		return nil, 0, false
	}

	return equation, len(d.open) + end + len(d.close), true
}
