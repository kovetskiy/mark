package parser

import "github.com/yuin/goldmark/v2/parser"

// Body is how the parse read the lines after an admonition's opening line.
type Body struct {
	// Level is whether the first of them was level with the opening line.
	Level bool

	// BlankFirst is whether a blank line came before that one.
	BlankFirst bool

	// Indent and Length are the column and the length of the opening run.
	Indent, Length int

	// ClosedAt is where the line of the closing run that closed the
	// admonition starts, and 0 when none did.
	ClosedAt int
}

// BodyOf returns how the parse read n's body.
func BodyOf(n *Admonition) Body {
	return Body{
		Level:      n.body.level,
		BlankFirst: n.body.blankFirst,
		Indent:     n.body.indent,
		Length:     n.body.length,
		ClosedAt:   n.body.closedAt,
	}
}

// WaitingWorkOf returns how many admonitions waiting for a closing run the
// parse that pc belongs to looked at.
func WaitingWorkOf(pc parser.Context) int {
	w, _ := pc.Get(waitingKey).(*waiting)
	if w == nil {
		return 0
	}

	return w.looked
}
