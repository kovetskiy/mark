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
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/internal/htmlblock"
	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Admonition is a MkDocs admonition block:
//
//	!!! note "Title" {.class #id key=value}
//	    Body, indented under the opening line.
//
// A body level with the opening line is one only up to a closing run:
//
//	!!! note "Title"
//	Body.
//	!!!
//
// Without the run the admonition ends at its opening line.
type Admonition struct {
	ast.BaseBlock

	// AdmonitionClass is the word after the opening run, "note" above.
	AdmonitionClass []byte

	// Title is the rest of the opening line before any attributes, quotes
	// included.
	Title []byte
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

	// length is the number of '!' that opened it; a closing run has to be at
	// least as long.
	length int

	// contentIndent is the indentation of the first non-blank line of the
	// body, which every later body line is read relative to.
	contentIndent     int
	contentHasStarted bool

	// dedented is set once a line indented less than the body came. The
	// admonition is still open after it only as long as that line and the
	// ones after it continue a paragraph of its body.
	dedented bool
}

// indented reports whether the body is indented under the opening line, and
// so ends at the first line that is not.
func (s *admonitionState) indented() bool {
	return s.contentHasStarted && s.contentIndent > s.indent
}

// admonitionStackKey holds the admonitions open at the current line,
// outermost first, as []*admonitionState.
var admonitionStackKey = parser.NewContextKey()

func admonitionStack(pc parser.Context) []*admonitionState {
	stack, _ := pc.Get(admonitionStackKey).([]*admonitionState)
	return stack
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
	indent := pos

	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}
	length := i - pos
	if length < 3 {
		return nil, parser.NoChildren
	}

	// A run of '!' with nothing after it is not an opening line: there would
	// be no telling whether a later bare run closes this admonition or opens
	// another.
	content := withoutLineEnding(line)
	left := i + util.TrimLeftSpaceLength(content[i:])
	if left >= len(content) {
		return nil, parser.NoChildren
	}

	node := parseAdmonitionOpeningLine(reader, left)

	state := &admonitionState{
		node:   node,
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

	first := !state.contentHasStarted && !util.IsBlank(line[pos:])
	if first {
		state.contentHasStarted = true
		state.contentIndent = w
	}

	closing, newline := hasAdmonitionClosingRun(line, w, pos, state)
	if closing && !runIsBodyText(state, stack[level+1:], runLength(line[pos:]), reader, pc) {
		// Len counts the padding already, which Advance steps over first.
		reader.Advance(segment.Len() - newline)

		return parser.Close
	}

	// A body that is not indented is one only up to a closing run. With none
	// ahead the admonition ends here, at its opening line, and this line is
	// read after it.
	if first && w == state.indent && !closingRunAhead(reader, pc, state.indent, state.length) {
		return parser.Close
	}

	// Without a closing run, a line ends the admonition when it is indented
	// less than the opening line, or level with it once the body was indented.
	if !util.IsBlank(line) && (w < state.indent || (w == state.indent && w < state.contentIndent)) {
		state.dedented = true

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
// as many '!' as opened it, at the opening line's indentation and with nothing
// after it. newline is 1 when the line ends in one, for the caller to leave it
// unread.
func hasAdmonitionClosingRun(line []byte, w int, pos int, state *admonitionState) (closing bool, newline int) {
	if w != state.indent {
		return false, 0
	}

	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}
	if i-pos < state.length || !util.IsBlank(line[i:]) {
		return false, 0
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		return true, 1
	}
	return true, 0
}

// runIsBodyText reports whether the line the reader is at, which reads as
// state's closing run and is run '!' long, is not its to take. Only a body
// level with the opening line can hold such a line: an indented body ends at
// it whatever is open inside.
//
// The run closes the admonition opened in that body and still open, when
// that one is level with this one. And it is text inside a fenced code block
// or an HTML block of that body that runs on over blank lines. An HTML block
// that ends at a blank line, such as "</details>", does not keep the run
// that follows it.
func runIsBodyText(state *admonitionState, above []*admonitionState, run int, reader text.Reader, pc parser.Context) bool {
	if state.indented() {
		return false
	}

	if len(above) > 0 && above[0].node.Parent() == state.node && above[0].indent == 0 {
		switch inner := above[0]; {
		case inner.indented():
			// It takes the run that comes straight after its body.
			return !inner.dedented && run >= inner.length
		case inner.contentHasStarted:
			return true
		default:
			// Its body starts on this line: the run closes it, or is text of
			// a body that a later run closes. With neither, it ends at its
			// opening line and the run is not its.
			return run >= inner.length || closingRunAhead(reader, pc, state.indent, inner.length)
		}
	}

	last := pc.LastOpenedBlock().Node
	if last == nil || last.Parent() != state.node {
		return false
	}

	switch block := last.(type) {
	case *ast.CodeBlock:
		return block.CodeBlockKind == ast.CodeBlockKindFenced
	case *ast.HTMLBlock:
		return block.HTMLBlockKind <= ast.HTMLBlockKind5 && !htmlBlockHasEnded(block, reader.Source())
	}

	return false
}

// htmlBlockHasEnded reports whether an open HTML block ends before the next
// line. goldmark closes one that ends on its first line only when it reads
// the line after.
func htmlBlockHasEnded(block *ast.HTMLBlock, source []byte) bool {
	segments := block.Value.Segments()

	return len(segments) == 1 && htmlblock.Closes(block.HTMLBlockKind, segments[0].Bytes(source))
}

// htmlBlockKind returns the kind of HTML block that line, without its
// indentation, starts when it is one of CommonMark's first five, the ones
// that run on over blank lines, and 0 for any other line.
func htmlBlockKind(line []byte) ast.HTMLBlockKind {
	switch {
	case bytes.HasPrefix(line, []byte("<!--")):
		return ast.HTMLBlockKind2
	case bytes.HasPrefix(line, []byte("<![CDATA[")):
		return ast.HTMLBlockKind5
	case bytes.HasPrefix(line, []byte("<?")):
		return ast.HTMLBlockKind3
	case len(line) > 2 && line[0] == '<' && line[1] == '!' && isASCIILetter(line[2]):
		return ast.HTMLBlockKind4
	}

	for _, tag := range []string{"<pre", "<script", "<style", "<textarea"} {
		if len(line) < len(tag) || !bytes.EqualFold(line[:len(tag)], []byte(tag)) {
			continue
		}

		if rest := line[len(tag):]; len(rest) == 0 || util.IsSpace(rest[0]) || rest[0] == '>' {
			return ast.HTMLBlockKind1
		}
	}

	return 0
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// closingRuns is what one parse has read ahead about the closing runs of its
// document.
type closingRuns struct {
	// last is where the last line that could close an admonition starts: a
	// run of '!' and nothing else, quote markers and indentation aside. It is
	// -1 without one. No admonition whose body starts after it has a closing
	// run.
	last int

	// ends tells, by where its first body line starts, where the closing run
	// of an admonition ends, or -1, so that a document of admonitions is not
	// read to its last run once for each.
	ends map[int]int
}

// closingRunsKey holds the *closingRuns of the document being parsed.
var closingRunsKey = parser.NewContextKey()

func documentClosingRuns(pc parser.Context, source []byte) *closingRuns {
	if runs, ok := pc.Get(closingRunsKey).(*closingRuns); ok {
		return runs
	}

	runs := &closingRuns{last: -1, ends: map[int]int{}}

	for start := 0; start < len(source); {
		end := lineEnd(source, start)
		line := bytes.TrimLeft(source[start:end], " \t>")

		if n := runLength(line); n >= 3 && util.IsBlank(line[n:]) {
			runs.last = start
		}

		start = end
	}

	pc.Set(closingRunsKey, runs)

	return runs
}

// lineEnd returns where the line that starts at start ends, its newline
// included.
func lineEnd(source []byte, start int) int {
	if i := bytes.IndexByte(source[start:], '\n'); i >= 0 {
		return start + i + 1
	}

	return len(source)
}

// runLength returns how many times line repeats its first byte before
// anything else.
func runLength(line []byte) int {
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}

	return n
}

// withoutContainerPrefix returns line without what the list items and
// blockquotes around an admonition take off it, given as prefix, which is
// what they took off its first body line. ok is false for a line outside
// them. The blank after a quote marker is optional, on either line; a blank
// line stays in a list item, and ends a quotation.
func withoutContainerPrefix(line []byte, prefix []byte) (rest []byte, ok bool) {
	for len(prefix) > 0 {
		if len(line) == 0 || line[0] != prefix[0] {
			return line, util.IsBlank(line) && bytes.IndexByte(prefix, '>') < 0
		}

		quote := prefix[0] == '>'
		line, prefix = line[1:], prefix[1:]

		if quote {
			line = bytes.TrimPrefix(line, []byte(" "))
			prefix = bytes.TrimPrefix(prefix, []byte(" "))
		}
	}

	return line, true
}

// closingRunAhead reports whether an admonition has a closing run further
// down: one opened by a run of length '!' at indent, whose body starts level
// with its opening line on the line the reader is at.
func closingRunAhead(reader text.Reader, pc parser.Context, indent int, length int) bool {
	source := reader.Source()
	_, segment := reader.PeekLine()
	start := bytes.LastIndexByte(source[:segment.Start], '\n') + 1

	body := bodyLines{
		source:  source,
		prefix:  source[start:segment.Start],
		padding: segment.Padding,
		column:  reader.LineOffset() + segment.Padding,
		indent:  indent,
		runs:    documentClosingRuns(pc, source),
	}

	return body.closingRun(start, length) >= 0
}

// bodyLines reads the lines of a body that is level with its opening line,
// as Continue will be given them: the ones in the same list item or
// blockquote, without what those take off each line.
type bodyLines struct {
	source []byte

	// prefix is what the list items and blockquotes took off the first body
	// line, padding the columns of a tab they took only part of, which count
	// as indentation, and column where the line starts after them.
	prefix  []byte
	padding int
	column  int

	// indent is the indentation of the opening line.
	indent int

	runs *closingRuns
}

// closingRun returns where the closing run of an admonition ends, or -1 when
// it has none: the admonition opened by a run of length '!' whose body
// starts on the line at start.
func (b *bodyLines) closingRun(start int, length int) int {
	if end, ok := b.runs.ends[start]; ok {
		return end
	}

	end := b.findClosingRun(start, length)
	b.runs.ends[start] = end

	return end
}

// findClosingRun reads on from start for the run that closes an admonition
// opened by length '!'. A line inside a fenced code block or an HTML block
// that runs on over blank lines is not one, and a run closes the nearest
// admonition above it that is still open: one opened in this body takes the
// run after its own unindented body, or straight after its indented one.
func (b *bodyLines) findClosingRun(start int, length int) int {
	var (
		// opened is the opening run of an admonition whose body has not
		// started, and indented that of one whose indented body the last
		// line was in.
		opened   int
		indented int

		fence       byte
		fenceLength int
		html        ast.HTMLBlockKind
	)

	for start < len(b.source) && start <= b.runs.last {
		lineStart := start
		start = lineEnd(b.source, lineStart)

		line, ok := withoutContainerPrefix(b.source[lineStart:start], b.prefix)
		if !ok {
			return -1
		}

		if util.IsBlank(line) {
			continue
		}

		w, pos := util.IndentWidth(line, b.column)
		if w += b.padding; w < b.indent {
			return -1
		}

		line = line[pos:]

		if html != 0 {
			if htmlblock.Closes(html, line) {
				html = 0
			}

			continue
		}

		run := runLength(line)
		bare := util.IsBlank(line[run:])

		if fenceLength > 0 {
			if line[0] == fence && run >= fenceLength && bare && w <= b.indent+3 {
				fenceLength = 0
			}

			continue
		}

		// A deeper line is in a list item, a code block or an indented body,
		// and neither opens nor closes anything here.
		if w > b.indent {
			if opened > 0 {
				indented, opened = opened, 0
			}

			continue
		}

		above := indented
		indented = 0

		if opened > 0 {
			nested := opened
			opened = 0

			// The admonition opened above has its body here. With a closing
			// run it holds the lines up to it; with none it ended at its
			// opening line, and neither does this one have a run when it
			// needs one as long.
			if end := b.closingRun(lineStart, nested); end >= 0 {
				start = end

				continue
			} else if length >= nested {
				return -1
			}
		}

		kind := htmlBlockKind(line)

		switch {
		case (line[0] == '`' || line[0] == '~') && run >= 3 && (line[0] == '~' || bytes.IndexByte(line[run:], '`') < 0):
			fence, fenceLength = line[0], run
		case kind != 0:
			if !htmlblock.Closes(kind, line) {
				html = kind
			}
		case line[0] != '!' || run < 3:
		case !bare:
			opened = run
		case above > 0 && run >= above:
			// It closes the admonition with the indented body above it.
		case run >= length:
			return start
		}
	}

	return -1
}

// attributesEndLine reports whether an attribute block in goldmark v1's
// grammar starts at the reader's position, blanks aside, and runs to the end
// of its line.
func attributesEndLine(source []byte, at text.Segment) bool {
	end, _, ok := syntax.AttributeBlock(source, at.Start)

	return ok && util.IsBlank(source[end:max(end, at.Stop)])
}
