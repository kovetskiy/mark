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
	"strings"

	"github.com/kovetskiy/mark/v17/internal/parsenote"
	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Admonition is a MkDocs admonition block. Its body is the lines indented
// under its opening line:
//
//	!!! note "Title" {.class #id key=value}
//	    Body, indented under the opening line.
//
// or the lines level with it up to a closing run, a line of at least as many
// '!' in the opening line's column:
//
//	!!! note "Title"
//	Body, level with the opening line.
//	!!!
//
// Lines level with it that no closing run ends are not its body: it ends at
// its opening line, and they follow it.
type Admonition struct {
	ast.BaseBlock

	// AdmonitionClass is the word after the opening run, "note" above.
	AdmonitionClass []byte

	// Title is the rest of the opening line before any attributes, quotes
	// included.
	Title []byte

	// body is how the parse read the lines after the opening line.
	body admonitionBody
}

// admonitionBody is what the parser keeps about one admonition between
// lines.
type admonitionBody struct {
	// parent is the block the admonition opened in: a level body is parsed
	// there, and its closing run has to be read there too.
	parent ast.Node

	// indent is the column of the opening line, and so of the closing one.
	indent int

	// length is the number of '!' that opened it; a closing run has to be at
	// least as long.
	length int

	// contentIndent is the column of the first line of an indented body,
	// which every later body line is read relative to, and 0 until there is
	// one.
	contentIndent int

	// started is set at the first non-blank line after the opening line, and
	// blankFirst when a blank line came before that one.
	started, blankFirst bool

	// level is set when that line is level with the opening line: the
	// admonition ends at its opening line, and waits in parent for a closing
	// run to be given the blocks parsed there up to it.
	level bool

	// ended is set while the last non-blank line given to an indented body
	// was level with the opening line, which goldmark only keeps on a
	// paragraph of the body: the body ends with that paragraph.
	ended bool

	// closedAt is where the line of the closing run that closed the
	// admonition starts, and 0 when none did.
	closedAt int
}

// Dump implements ast.Node.
func (n *Admonition) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"AdmonitionClass": string(n.AdmonitionClass),
		"Title":           string(n.Title),
	})
}

// KindAdmonition is the ast.NodeKind of an Admonition.
var KindAdmonition = ast.NewNodeKind("Admonition")

// Kind implements ast.Node.
func (n *Admonition) Kind() ast.NodeKind {
	return KindAdmonition
}

// NewAdmonition returns an empty Admonition.
func NewAdmonition() *Admonition {
	n := &Admonition{}
	n.Init(n)
	return n
}

// closingRunBlock is the line of a closing run that closed an admonition with
// a level body. goldmark reads a line only as part of a block, so the line is
// one until the next line is read, and then it takes itself out of the tree.
type closingRunBlock struct {
	ast.BaseBlock
}

var kindClosingRun = ast.NewNodeKind("AdmonitionClosingRun")

// Kind implements ast.Node.
func (n *closingRunBlock) Kind() ast.NodeKind {
	return kindClosingRun
}

// Dump implements ast.Node.
func (n *closingRunBlock) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, nil)
}

type admonitionParser struct{}

// NewAdmonitionParser returns a block parser for MkDocs admonitions. Register
// NewAdmonitionHTMLBlockParser beside it, so that a closing run ends an HTML
// block that would otherwise go on to the next blank line.
func NewAdmonitionParser() parser.BlockParser {
	return &admonitionParser{}
}

// waitingKey holds the parse's *waiting.
var waitingKey = parser.NewContextKey()

// waiting holds, for each block that admonitions with a level body opened in,
// those no closing run has closed yet, innermost last, a stack for each
// column an opening line can start in: 0 to 3, a line indented further being
// code.
type waiting struct {
	in  map[ast.Node]*[4][]waitingAdmonition
	seq int

	// looked counts the admonitions looked at, which bounds what reading
	// closing runs costs.
	looked int
}

type waitingAdmonition struct {
	node *Admonition

	// seq orders the admonitions of one block across its stacks.
	seq int

	// shortest is the shortest opening run of this one and those under it,
	// so that a run too short for every one is told so without a look at
	// each.
	shortest int
}

func waitingIn(pc parser.Context) *waiting {
	w, _ := pc.Get(waitingKey).(*waiting)
	if w == nil {
		w = &waiting{in: map[ast.Node]*[4][]waitingAdmonition{}}
		pc.Set(waitingKey, w)
	}

	return w
}

// push adds n to those waiting in the block it opened in.
func (w *waiting) push(n *Admonition) {
	stacks := w.in[n.body.parent]
	if stacks == nil {
		stacks = &[4][]waitingAdmonition{}
		w.in[n.body.parent] = stacks
	}

	stack := stacks[n.body.indent]
	shortest := n.body.length
	if len(stack) > 0 {
		shortest = min(shortest, stack[len(stack)-1].shortest)
	}

	w.seq++
	stacks[n.body.indent] = append(stack, waitingAdmonition{node: n, seq: w.seq, shortest: shortest})
}

// innermost returns the place on its stack of the innermost admonition
// waiting in parent that a run of length '!' in column indent closes, and -1
// for none.
func (w *waiting) innermost(parent ast.Node, indent, length int) int {
	stacks := w.in[parent]
	if stacks == nil || indent < 0 || indent >= len(stacks) {
		return -1
	}

	stack := stacks[indent]
	if len(stack) == 0 || stack[len(stack)-1].shortest > length {
		return -1
	}

	for i := len(stack) - 1; ; i-- {
		w.looked++
		if stack[i].node.body.length <= length {
			return i
		}
	}
}

// top returns the innermost admonition waiting in parent with its opening
// line in column indent, or nil.
func (w *waiting) top(parent ast.Node, indent int) *Admonition {
	stacks := w.in[parent]
	if stacks == nil || indent < 0 || indent >= len(stacks) || len(stacks[indent]) == 0 {
		return nil
	}

	return stacks[indent][len(stacks[indent])-1].node
}

// close takes the admonition at i of the stack of column indent from those
// waiting in parent, with each opened there after it, which it now holds, and
// returns it.
func (w *waiting) close(parent ast.Node, indent, i int) *Admonition {
	stacks := w.in[parent]
	closed := stacks[indent][i]
	stacks[indent] = stacks[indent][:i]

	for column := range stacks {
		for len(stacks[column]) > 0 && stacks[column][len(stacks[column])-1].seq > closed.seq {
			w.looked++
			stacks[column] = stacks[column][:len(stacks[column])-1]
		}
	}

	return closed.node
}

func (b *admonitionParser) Trigger() []byte {
	return []byte{'!'}
}

func (b *admonitionParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || line[pos] != '!' {
		return nil, parser.NoChildren
	}
	indent := pc.BlockIndent()

	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}
	length := i - pos
	if length < 3 {
		return nil, parser.NoChildren
	}

	// A run of '!' with nothing after it opens nothing: it closes an
	// admonition, or is text.
	content := withoutLineEnding(line)
	left := i + util.TrimLeftSpaceLength(content[i:])
	if left >= len(content) {
		return openClosingRun(parent, reader, pc, indent, length)
	}

	node := parseAdmonitionOpeningLine(reader, left)
	node.body = admonitionBody{parent: parent, indent: indent, length: length}

	line, _ = reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if run, _ := node.body.closingRun(line, w, pos); w < indent || run > 0 {
		return node, parser.NoChildren
	}

	return node, parser.HasChildren
}

// openClosingRun reads a line of nothing but a run of length '!' in column
// indent of parent. It closes the innermost admonition waiting there that it
// is long enough for, which is given the blocks parsed in parent after it.
// The line closing nothing, it is no block, and goes on a paragraph or starts
// one.
func openClosingRun(parent ast.Node, reader text.Reader, pc parser.Context, indent, length int) (ast.Node, parser.State) {
	w, _ := pc.Get(waitingKey).(*waiting)
	if w == nil {
		return nil, parser.NoChildren
	}

	i := w.innermost(parent, indent, length)
	if i < 0 {
		return nil, parser.NoChildren
	}

	node := w.close(parent, indent, i)
	for child := node.NextSibling(); child != nil; {
		next := child.NextSibling()
		node.AppendChild(child)
		child = next
	}

	parsenote.Withdraw(pc, node.Pos())

	line, segment := reader.PeekLine()
	node.body.closedAt = segment.Start
	reader.Advance(segment.Len() - newlineLength(line))

	run := &closingRunBlock{}
	run.Init(run)

	return run, parser.NoChildren
}

func (b *admonitionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n, ok := node.(*Admonition)
	if !ok {
		return parser.Close
	}
	body := &n.body

	line, segment := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	run, newline := body.closingRun(line, w, pos)

	switch {
	case util.IsBlank(line):
		body.blankFirst = body.blankFirst || !body.started

	case !body.started:
		body.started = true

		switch {
		case run > 0:
			return body.closeAt(reader, segment, newline)
		case w > body.indent:
			body.contentIndent = w
		case w < body.indent || n.HasChildren():
			// What the opening line left in it goes on in a level line
			// only as a paragraph does, lazily.
			body.ended = true
			return parser.Close
		default:
			body.level = true
			waitingIn(pc).push(n)
			if !body.blankFirst {
				parsenote.StrandedBody(pc, n.Pos())
			}

			return parser.Close
		}

	case run > 0 && !body.yields(pc, run):
		return body.closeAt(reader, segment, newline)

	case w <= body.indent:
		body.ended = true
		return parser.Close

	default:
		body.ended = false
	}

	if body.contentIndent > 0 {
		// Step over the body's indentation, and no further: a lazy line
		// indented less than the body keeps its text, and a tab counts as
		// the columns it spans rather than as one byte.
		if pos, padding := util.IndentPosition(line, reader.LineOffset(), body.contentIndent); pos >= 0 {
			reader.AdvanceAndSetPadding(pos, padding)
		} else {
			reader.Advance(util.TrimLeftSpaceLength(line[:len(line)-util.TrimRightSpaceLength(line)]))
		}
	}

	return parser.Continue | parser.HasChildren
}

// yields reports whether a closing run of length '!' that would close this
// admonition, whose body is indented, goes to an admonition around it
// instead: the innermost waiting for one in the same block, when the body
// already ended at a line level with the opening line, or the one around it
// when it was opened with more '!' and the run has as many.
func (b *admonitionBody) yields(pc parser.Context, length int) bool {
	w, _ := pc.Get(waitingKey).(*waiting)
	if w == nil {
		return false
	}

	if b.ended {
		return w.innermost(b.parent, b.indent, length) >= 0
	}

	outer := w.top(b.parent, b.indent)

	return outer != nil && outer.body.length > b.length && outer.body.length <= length
}

// closeAt ends the admonition at the closing run on the reader's line.
func (b *admonitionBody) closeAt(reader text.Reader, segment text.Segment, newline int) parser.State {
	b.closedAt = segment.Start
	// Len counts the padding already, which Advance steps over first.
	reader.Advance(segment.Len() - newline)

	return parser.Close
}

// Close takes the line of a closing run out of the tree once the next line
// is read; an admonition needs nothing done.
func (b *admonitionParser) Close(node ast.Node, _ text.Reader, _ parser.Context) {
	if run, ok := node.(*closingRunBlock); ok && run.Parent() != nil {
		run.Parent().RemoveChild(run)
	}
}

func (b *admonitionParser) CanInterruptParagraph() bool {
	return true
}

func (b *admonitionParser) CanAcceptIndentedLine() bool {
	return false
}

// admonitionHTMLBlockParser is goldmark's HTML block parser, but for a block
// that ends at a blank line: a line of a closing run that closes an
// admonition waiting in the block around it ends it, as it ends a paragraph.
type admonitionHTMLBlockParser struct {
	parser.BlockParser
}

// NewAdmonitionHTMLBlockParser returns goldmark's HTML block parser with a
// closing run ending a block like <div> or </details> that would otherwise go
// on to the next blank line. Register it beside NewAdmonitionParser, ahead of
// goldmark's own (at a priority below 900). A run in a <pre>, <script>,
// <style> or <textarea> block, a comment, a declaration or CDATA stays the
// block's text, as one in a fenced code block does.
func NewAdmonitionHTMLBlockParser() parser.BlockParser {
	return admonitionHTMLBlockParser{parser.NewHTMLBlockParser()}
}

func (b admonitionHTMLBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	html, ok := node.(*ast.HTMLBlock)
	if !ok || (html.HTMLBlockKind != ast.HTMLBlockKind6 && html.HTMLBlockKind != ast.HTMLBlockKind7) {
		return b.BlockParser.Continue(node, reader, pc)
	}

	if w, _ := pc.Get(waitingKey).(*waiting); w != nil {
		line, _ := reader.PeekLine()
		indent, pos := util.IndentWidth(line, reader.LineOffset())
		if length := bareRun(line, pos); length > 0 && w.innermost(node.Parent(), indent, length) >= 0 {
			return parser.Close
		}
	}

	return b.BlockParser.Continue(node, reader, pc)
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

	// The block has to end the opening line, in goldmark v1's grammar (see
	// syntax.AttributeBlock): goldmark v2's would take "{x}" and read on into the
	// body's first line for one.
	var attributes []ast.Attribute

	ok := false
	if _, at := reader.Position(); attributesEndLine(reader.Source(), at) {
		attributes, ok = parser.ParseAttributes(reader)
	}

	// ParseAttributes skips blanks before the '{', and a newline is one, so
	// on a line with no attributes it reads into the next line before putting
	// the position back. Putting it back does not drop the line it peeked, and
	// goldmark then takes that line for the rest of this one: inside a
	// blockquote it opened a second quote on the same line and panicked.
	// Advancing by nothing drops the stale line.
	reader.Advance(0)

	if ok {
		for _, attribute := range attributes {
			// goldmark reads every attribute value as text: {width=5} is "5"
			// and {open=true} is "true".
			if !admonitionAttributeIsText(attribute, reader.Source()) {
				continue
			}

			if attribute.Name == "class" {
				hasClass = true
				value := string(class) + " " + attribute.Value.Value(reader.Source())
				node.SetAttribute(attribute.Name, text.NewMultiLineValueFromString(value, text.IdentityDecoder))

				continue
			}

			node.SetAttribute(attribute.Name, attribute.Value)
		}
	}

	if !hasClass {
		node.SetAttribute("class", text.NewMultiLineValueFromString(string(class), text.IdentityDecoder))
	}

	return node
}

// admonitionAttributeIsText reports whether an attribute value is one an HTML
// attribute can carry: an unquoted [1,2], {a=b} or null is a list, an object
// or nothing, written in a syntax an HTML attribute has no spelling for, so it
// is left out.
func admonitionAttributeIsText(attribute ast.Attribute, source []byte) bool {
	value := attribute.Value
	if value.IsOwned() {
		return true
	}

	start := value.Index().Start
	if start > 0 && (source[start-1] == '"' || source[start-1] == '\'') {
		return true
	}

	raw := value.Str(source)

	return raw != "null" && !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{")
}

// withoutLineEnding returns line without the newline it ends in, if it ends in
// one. The last line of a document need not.
func withoutLineEnding(line []byte) []byte {
	return bytes.TrimSuffix(line, []byte("\n"))
}

// newlineLength is 1 when line ends in a newline, for the caller to leave it
// unread.
func newlineLength(line []byte) int {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		return 1
	}

	return 0
}

// bareRun returns the length of the run of '!' at pos in line when it is at
// least three long and nothing but blanks follows it, and 0 otherwise.
func bareRun(line []byte, pos int) int {
	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}

	if i-pos < 3 || !util.IsBlank(line[i:]) {
		return 0
	}

	return i - pos
}

// closingRun returns the length of the run on line, w columns in with its
// first '!' at pos, when it closes b: as many '!' as opened it or more, in the
// opening line's column, with nothing after them. newline is 1 when the line
// ends in one.
func (b *admonitionBody) closingRun(line []byte, w, pos int) (length, newline int) {
	if w != b.indent {
		return 0, 0
	}

	if length = bareRun(line, pos); length < b.length {
		return 0, 0
	}

	return length, newlineLength(line)
}

// attributesEndLine reports whether an attribute block in goldmark v1's
// grammar starts at the reader's position, blanks aside, and runs to the end
// of its line.
func attributesEndLine(source []byte, at text.Segment) bool {
	end, _, ok := syntax.AttributeBlock(source, at.Start)

	return ok && util.IsBlank(source[end:max(end, at.Stop)])
}
