// The admonition parser started as github.com/stefanfritsch/goldmark-admonitions
// v1.1.1, which is no longer maintained:
//
// Copyright (c) 2019 Yusuke Inuzuka
// Copyright (c) 2022 Stefan Fritsch
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package parser

import (
	"bytes"
	"fmt"
	"slices"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Admonition is a MkDocs admonition block:
//
//	!!! note "Title" {.class #id key=value}
//	    Body, indented under the opening line.
//	!!!
//
// MkDocs also writes a collapsible one with '???', which starts collapsed, or
// '???+', which starts open. They close with a run of '?' the way '!!!' closes
// with a run of '!'.
type Admonition struct {
	ast.BaseBlock

	// AdmonitionClass is the word after the opening run, "note" above.
	AdmonitionClass []byte

	// Title is the rest of the opening line before any attributes, quotes
	// included.
	Title []byte

	// Collapsed is true for a block opened with '???'. A '???+' block starts
	// open, which is how a '!!!' block always reads, so it is not Collapsed.
	Collapsed bool
}

// Dump implements ast.Node.
func (n *Admonition) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"AdmonitionClass": string(n.AdmonitionClass),
		"Title":           string(n.Title),
		"Collapsed":       fmt.Sprint(n.Collapsed),
	}, nil)
}

// KindAdmonition is the ast.NodeKind of an Admonition.
var KindAdmonition = ast.NewNodeKind("Admonition")

// Kind implements ast.Node.
func (n *Admonition) Kind() ast.NodeKind {
	return KindAdmonition
}

// NewAdmonition returns an empty Admonition.
func NewAdmonition() *Admonition {
	return &Admonition{}
}

type admonitionParser struct{}

// NewAdmonitionParser returns a block parser for MkDocs admonitions.
func NewAdmonitionParser() parser.BlockParser {
	return &admonitionParser{}
}

// admonitionState is what the parser keeps about one open admonition between
// lines.
type admonitionState struct {
	node *Admonition

	// indent is the indentation of the opening line, and so of the closing one.
	indent int

	// marker is the character of the run that opened it, '!' or '?', and the
	// only one a closing run is made of.
	marker byte

	// length is the number of marker characters that opened it; a closing run
	// has to be at least as long. The '+' of '???+' is not counted.
	length int

	// contentIndent is the indentation of the first non-blank line of the
	// body, which every later body line is read relative to.
	contentIndent     int
	contentHasStarted bool
}

// admonitionStackKey holds the admonitions open at the current line,
// outermost first, as []*admonitionState.
var admonitionStackKey = parser.NewContextKey()

func admonitionStack(pc parser.Context) []*admonitionState {
	stack, _ := pc.Get(admonitionStackKey).([]*admonitionState)
	return stack
}

func (b *admonitionParser) Trigger() []byte {
	return []byte{'!', '?'}
}

func (b *admonitionParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos >= len(line) || (line[pos] != '!' && line[pos] != '?') {
		return nil, parser.NoChildren
	}
	indent := pos
	marker := line[pos]

	i := pos
	for ; i < len(line) && line[i] == marker; i++ {
	}
	length := i - pos
	if length < 3 {
		return nil, parser.NoChildren
	}

	// '???+' is the open spelling of '???'. The '+' belongs to the opening run.
	collapsed := marker == '?'
	if collapsed && i < len(line) && line[i] == '+' {
		collapsed = false
		i++
	}

	// A run of '!' or '?' with nothing after it is not an opening line: there would
	// be no telling whether a later bare run closes this admonition or opens
	// another.
	content := withoutLineEnding(line)
	left := i + util.TrimLeftSpaceLength(content[i:])
	if left >= len(content) {
		return nil, parser.NoChildren
	}

	// '???' and '????' open prose far more often than '!!!' does ("??? what is
	// this"), and an opening line swallows its text. MkDocs only writes a title
	// in quotes, so after the class a '?' run takes a quoted title, attributes,
	// or nothing; anything else is a sentence.
	if marker == '?' {
		bare, ok := admonitionTitleOrNothing(content[left:])
		if !ok {
			return nil, parser.NoChildren
		}

		// "??? what" followed by an unindented line is a sentence, not an
		// empty admonition that swallows the paragraph under it. Only a
		// document's own lines can be read this way: inside a list or a
		// blockquote the next line carries a prefix that is not ours to judge.
		if bare && parent.Kind() == ast.KindDocument && !hasAdmonitionBodyAhead(reader, indent, marker, length) {
			return nil, parser.NoChildren
		}
	}

	node := parseAdmonitionOpeningLine(reader, left)
	node.Collapsed = collapsed

	state := &admonitionState{
		node:   node,
		marker: marker,
		indent: indent,
		length: length,
	}
	pc.Set(admonitionStackKey, append(admonitionStack(pc), state))

	line, _ = reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if closing, _ := hasAdmonitionClosingRun(line, w, pos, state); w < state.indent || closing {
		return node, parser.NoChildren
	}

	return node, parser.HasChildren
}

// admonitionTitleOrNothing reports whether what follows the opening run, class
// first, is a class and then nothing, a quoted title, attributes, or both. bare
// is true when the class stands alone. The class ends where
// parseAdmonitionOpeningLine ends it, at a space or a '{', and has to be a
// word: "Really?" and `note"T"` are prose.
func admonitionTitleOrNothing(rest []byte) (bare bool, ok bool) {
	end := 0
	for ; end < len(rest) && rest[end] != ' ' && rest[end] != '{'; end++ {
	}
	if end == 0 || !isAdmonitionClass(rest[:end]) {
		return false, false
	}

	rest = bytes.TrimLeft(rest[end:], " ")
	if len(rest) == 0 {
		return true, true
	}

	if rest[0] == '"' {
		// A title is one quoted string, closed on this line.
		closed := false
		i := 1
		for ; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
			} else if rest[i] == '"' {
				closed = true
				i++
				break
			}
		}
		if !closed {
			return false, false
		}
		rest = bytes.TrimLeft(rest[i:], " ")
		return false, len(rest) == 0 || rest[0] == '{'
	}

	return false, rest[0] == '{'
}

// isAdmonitionClass reports whether class is made of letters, digits, '-' and
// '_'.
func isAdmonitionClass(class []byte) bool {
	for _, r := range string(class) {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// hasAdmonitionBodyAhead reports whether the first non-blank line after the one
// being read is indented deeper than the opening line, or is the run that
// closes it. That is what an admonition with a body, or an empty one, looks
// like.
func hasAdmonitionBodyAhead(reader text.Reader, indent int, marker byte, length int) bool {
	_, segment := reader.PeekLine()
	source := reader.Source()
	state := &admonitionState{marker: marker, indent: indent, length: length}

	for rest := source[min(segment.Stop, len(source)):]; len(rest) > 0; {
		line := rest
		if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
			line, rest = rest[:nl+1], rest[nl+1:]
		} else {
			rest = nil
		}
		if util.IsBlank(line) {
			continue
		}

		w, pos := util.IndentWidth(line, 0)
		if closing, _ := hasAdmonitionClosingRun(line, w, pos, state); closing {
			return true
		}
		return w > indent
	}

	return false
}

// parseAdmonitionOpeningLine reads the class, title and attributes of an
// opening line, starting left bytes into it.
func parseAdmonitionOpeningLine(reader text.Reader, left int) *Admonition {
	node := NewAdmonition()
	reader.Advance(left)

	remainingLine, _ := reader.PeekLine()
	remainingLine = withoutLineEnding(remainingLine)
	remainingLength := len(remainingLine)

	endClass := 0
	for ; endClass < remainingLength && remainingLine[endClass] != ' ' && remainingLine[endClass] != '{'; endClass++ {
	}
	if endClass > 0 {
		node.AdmonitionClass = remainingLine[0:endClass]
	}

	startTitle := endClass + util.TrimLeftSpaceLength(remainingLine[endClass:])
	endTitle := startTitle
	for ; endTitle < remainingLength && remainingLine[endTitle] != '{'; endTitle++ {
	}
	if endTitle > startTitle {
		endTitle -= util.TrimRightSpaceLength(remainingLine[startTitle:endTitle])
		if endTitle > startTitle {
			node.Title = remainingLine[startTitle:endTitle]
		}
	}

	if endTitle < remainingLength {
		reader.Advance(endTitle)
	} else {
		reader.Advance(remainingLength)
	}

	class := append([]byte("admonition adm-"), node.AdmonitionClass...)
	hasClass := false

	attributes, ok := parser.ParseAttributes(reader)

	// ParseAttributes skips blanks before the '{', and a newline is one, so
	// on a line with no attributes it reads into the next line before putting
	// the position back. Putting it back does not drop the line it peeked, and
	// goldmark then takes that line for the rest of this one: inside a
	// blockquote it opened a second quote on the same line and panicked.
	// Advancing by nothing drops the stale line.
	reader.Advance(0)

	if ok {
		for _, attribute := range attributes {
			value, ok := admonitionAttributeValue(attribute.Value)
			if !ok {
				continue
			}

			if bytes.Equal(attribute.Name, []byte("class")) {
				hasClass = true
				value = bytes.Join([][]byte{class, value}, []byte(" "))
			}

			node.SetAttribute(attribute.Name, value)
		}
	}

	if !hasClass {
		node.SetAttribute([]byte("class"), class)
	}

	return node
}

// admonitionAttributeValue spells an attribute value as text. goldmark reads
// {width=5} as a number and {open=true} as a bool, which the library this
// parser came from assumed could not happen, and panicked on. A list or a
// nested object has no spelling an HTML attribute could carry, so it is left
// out.
func admonitionAttributeValue(value any) ([]byte, bool) {
	switch v := value.(type) {
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	case float64, bool:
		return fmt.Append(nil, v), true
	default:
		return nil, false
	}
}

func (b *admonitionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	stack := admonitionStack(pc)
	level := -1
	for i, state := range stack {
		if state.node == node {
			level = i
			break
		}
	}
	if level < 0 {
		return parser.Close
	}
	state := stack[level]

	line, segment := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())

	if !state.contentHasStarted && !util.IsBlank(line[pos:]) {
		state.contentHasStarted = true
		state.contentIndent = w
	}

	// A closing run ends only the innermost admonition; an outer one closes
	// when its own run comes.
	closing, newline := hasAdmonitionClosingRun(line, w, pos, state)
	if closing && level == len(stack)-1 {
		// Len counts the padding already, which Advance steps over first.
		reader.Advance(segment.Len() - newline)
		return parser.Close
	}

	// Without a closing run, a line ends the admonition when it is indented
	// less than the opening line, or level with it once the body was indented.
	if !util.IsBlank(line) && (w < state.indent || (w == state.indent && w < state.contentIndent)) {
		return parser.Close
	}

	if state.contentIndent > 0 {
		// Step over the body's indentation, and no further: a lazy line
		// indented less than the body keeps its text, and a tab counts as
		// the columns it spans rather than as one byte.
		if pos, padding := util.IndentPosition(line, reader.LineOffset(), state.contentIndent); pos >= 0 {
			reader.AdvanceAndSetPadding(pos, padding)
		} else {
			reader.Advance(util.TrimLeftSpaceLength(line[:len(line)-util.TrimRightSpaceLength(line)]))
		}
	}

	return parser.Continue | parser.HasChildren
}

// Close drops the admonition from the stack. goldmark calls it however the
// block ended -- a closing run, a dedent, or the end of the list or blockquote
// around it -- so the stack cannot keep an admonition that is no longer open.
//
// Only this admonition's own entry goes. A sibling that opened on the line
// that ended this one is already on the stack above it by now, because
// goldmark opens the new block before it closes the old.
func (b *admonitionParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	stack := admonitionStack(pc)
	for i, state := range stack {
		if state.node == node {
			pc.Set(admonitionStackKey, slices.Delete(slices.Clone(stack), i, i+1))
			return
		}
	}
}

func (b *admonitionParser) CanInterruptParagraph() bool {
	return true
}

func (b *admonitionParser) CanAcceptIndentedLine() bool {
	return false
}

// withoutLineEnding returns line without the newline it ends in, if it ends in
// one. The last line of a document need not.
func withoutLineEnding(line []byte) []byte {
	return bytes.TrimSuffix(line, []byte("\n"))
}

// hasAdmonitionClosingRun reports whether line closes state: a run of at least
// as many of the opening marker ('!' or '?') as opened it, at the opening
// line's indentation and with nothing after it. newline is 1 when the line ends
// in one, for the caller to leave it unread.
func hasAdmonitionClosingRun(line []byte, w int, pos int, state *admonitionState) (closing bool, newline int) {
	if w != state.indent {
		return false, 0
	}

	i := pos
	for ; i < len(line) && line[i] == state.marker; i++ {
	}
	if i-pos < state.length || !util.IsBlank(line[i:]) {
		return false, 0
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		return true, 1
	}
	return true, 0
}
