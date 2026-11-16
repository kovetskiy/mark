// Package parsenote carries what a block parser found about a document that
// its author should hear, from the parse to the compile that reports it.
//
// A goldmark block parser returns a node and nothing else, so a finding
// travels in the context of the parse, under a key kept here. The package
// imports none of mark's, so a parser and what assembles the parsers can both
// use it.
package parsenote

import "github.com/yuin/goldmark/v2/parser"

var strandedBodiesKey = parser.NewContextKey()

// strandedBodies is where the admonitions recorded by StrandedBody start, in
// the order they were recorded, and which of them a closing run took back.
type strandedBodies struct {
	at        []int
	withdrawn map[int]bool
}

func strandedIn(pc parser.Context) *strandedBodies {
	stranded, _ := pc.Get(strandedBodiesKey).(*strandedBodies)
	if stranded == nil {
		stranded = &strandedBodies{withdrawn: map[int]bool{}}
		pc.Set(strandedBodiesKey, stranded)
	}

	return stranded
}

// StrandedBody records an admonition as one whose body is published after it
// rather than in it: the line that follows its opening line is level with it,
// and the admonition ended there. at is where the opening run starts in the
// source. A closing run found later withdraws it with Withdraw.
func StrandedBody(pc parser.Context, at int) {
	stranded := strandedIn(pc)
	stranded.at = append(stranded.at, at)
	delete(stranded.withdrawn, at)
}

// Withdraw takes back what StrandedBody recorded for the admonition at at: a
// closing run gave it the lines after it.
func Withdraw(pc parser.Context, at int) {
	if stranded, _ := pc.Get(strandedBodiesKey).(*strandedBodies); stranded != nil {
		stranded.withdrawn[at] = true
	}
}

// StrandedBodies returns where the admonitions recorded by StrandedBody and
// not withdrawn start, in the order they were recorded.
func StrandedBodies(pc parser.Context) []int {
	stranded, _ := pc.Get(strandedBodiesKey).(*strandedBodies)
	if stranded == nil {
		return nil
	}

	var at []int

	for _, start := range stranded.at {
		if !stranded.withdrawn[start] {
			at = append(at, start)
		}
	}

	return at
}
