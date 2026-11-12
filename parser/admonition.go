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
	if closing && !runIsBodyText(node, state, stack[level+1:], pc, reader.Source()) {
		// Len counts the padding already, which Advance steps over first.
		reader.Advance(segment.Len() - newline)

		return parser.Close
	}

	// A body that is not indented is one only up to a closing run. With none
	// ahead the admonition ends here, at its opening line, and this line is
	// read after it.
	if first && w == state.indent && !closingRunAhead(reader, pc, state) {
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

// runIsBodyText reports whether a line that reads as state's closing run is
// not its to take. Only a body level with the opening line can hold such a
// line: an indented body ends at it whatever is open inside.
//
// It is text inside a fenced code block or an HTML block of that body, and it
// closes the admonition opened in that body, when that one is waiting for a
// run of its own.
func runIsBodyText(node ast.Node, state *admonitionState, above []*admonitionState, pc parser.Context, source []byte) bool {
	if state.indented() {
		return false
	}

	if len(above) > 0 && above[0].node.Parent() == node && above[0].indent == 0 && !above[0].indented() {
		return true
	}

	last := pc.LastOpenedBlock().Node
	if last == nil || last.Parent() != node {
		return false
	}

	switch block := last.(type) {
	case *ast.CodeBlock:
		return block.CodeBlockKind == ast.CodeBlockKindFenced
	case *ast.HTMLBlock:
		return !htmlBlockHasEnded(block, source)
	}

	return false
}

// htmlBlockHasEnded reports whether an open HTML block ends before the next
// line. goldmark closes one that ends on its first line only when it reads
// the line after; every other open block takes any line that is not blank.
func htmlBlockHasEnded(block *ast.HTMLBlock, source []byte) bool {
	segments := block.Value.Segments()
	if len(segments) != 1 {
		return false
	}

	first := bytes.ToLower(bytes.TrimLeft(segments[0].Bytes(source), " "))

	return containsAny(first, htmlBlockEnds(first))
}

// htmlBlockEnds returns what ends the HTML block that line, in lower case and
// without its indentation, starts: one of CommonMark's first five kinds, the
// ones that run on over blank lines. It returns nil for any other line.
func htmlBlockEnds(line []byte) []string {
	switch {
	case bytes.HasPrefix(line, []byte("<!--")):
		return []string{"-->"}
	case bytes.HasPrefix(line, []byte("<![cdata[")):
		return []string{"]]>"}
	case bytes.HasPrefix(line, []byte("<?")):
		return []string{"?>"}
	case len(line) > 2 && line[0] == '<' && line[1] == '!' && util.IsAlphaNumeric(line[2]) && !util.IsNumeric(line[2]):
		return []string{">"}
	}

	for _, tag := range []string{"<pre", "<script", "<style", "<textarea"} {
		if rest, ok := bytes.CutPrefix(line, []byte(tag)); ok && (util.IsBlank(rest) || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '>') {
			return []string{"</script>", "</pre>", "</style>", "</textarea>"}
		}
	}

	return nil
}

func containsAny(line []byte, ends []string) bool {
	return slices.ContainsFunc(ends, func(end string) bool {
		return bytes.Contains(line, []byte(end))
	})
}

// lastBareRunKey holds the offset of the last line of the document that could
// close an admonition, as an int.
var lastBareRunKey = parser.NewContextKey()

// lastBareRun returns where the last line that is a run of '!' and nothing
// else starts, quote markers and indentation aside, or -1. No admonition
// opened after it has a closing run, which a document of title-only
// admonitions would otherwise be read to its end for, once for each.
func lastBareRun(pc parser.Context, source []byte) int {
	if at, ok := pc.Get(lastBareRunKey).(int); ok {
		return at
	}

	last := -1

	for start := 0; start < len(source); {
		end := lineEnd(source, start)
		line := bytes.TrimLeft(source[start:end], " \t>")

		if n := len(line) - len(bytes.TrimLeft(line, "!")); n >= 3 && util.IsBlank(line[n:]) {
			last = start
		}

		start = end
	}

	pc.Set(lastBareRunKey, last)

	return last
}

// lineEnd returns where the line that starts at start ends, its newline
// included.
func lineEnd(source []byte, start int) int {
	if i := bytes.IndexByte(source[start:], '\n'); i >= 0 {
		return start + i + 1
	}

	return len(source)
}

// closingRunAhead reports whether state, whose body starts level with its
// opening line on the line the reader is at, has a closing run further down.
//
// It reads the lines as Continue will be given them: the ones that start as
// this one does, which are the ones in the same list item or blockquote, and
// not the ones inside a fenced code block or an HTML block that runs on over
// blank lines. A run closes the nearest
// admonition above it that waits for one, so an admonition opened in this
// body whose own body is not indented takes the next run.
func closingRunAhead(reader text.Reader, pc parser.Context, state *admonitionState) bool {
	source := reader.Source()
	_, segment := reader.PeekLine()

	if segment.Start > lastBareRun(pc, source) {
		return false
	}

	start := bytes.LastIndexByte(source[:segment.Start], '\n') + 1
	prefix := source[start:segment.Start]
	blankPrefix := bytes.TrimRight(prefix, " \t")

	var (
		// waiting holds the opening runs of the admonitions opened in this
		// body that wait for a closing run, outermost first.
		waiting []int

		// opened is the opening run of an admonition whose body has not
		// started, and indentedBody whether the lines are those of one whose
		// body is indented.
		opened       int
		indentedBody bool

		fence       byte
		fenceLength int
		htmlEnds    []string
	)

	for start < len(source) {
		end := lineEnd(source, start)
		line := source[start:end]
		start = end

		if !bytes.HasPrefix(line, prefix) {
			if bytes.HasPrefix(line, blankPrefix) && util.IsBlank(line[len(blankPrefix):]) {
				continue
			}

			return false
		}

		line = line[len(prefix):]

		if htmlEnds != nil {
			if containsAny(bytes.ToLower(line), htmlEnds) {
				htmlEnds = nil
			}

			continue
		}

		if util.IsBlank(line) {
			continue
		}

		w, pos := util.IndentWidth(line, 0)
		run := len(line[pos:]) - len(bytes.TrimLeft(line[pos:], string(line[pos])))
		bare := util.IsBlank(line[pos+run:])

		if fenceLength > 0 {
			if line[pos] == fence && run >= fenceLength && bare {
				fenceLength = 0
			}

			continue
		}

		if w < state.indent {
			return false
		}

		if w > state.indent {
			if opened > 0 {
				opened, indentedBody = 0, true
			}
			if indentedBody || w > state.indent+3 {
				continue
			}
		} else {
			indentedBody = false

			if opened > 0 {
				waiting = append(waiting, opened)
				opened = 0
			}
		}

		if (line[pos] == '`' || line[pos] == '~') && run >= 3 && (line[pos] == '~' || !bytes.Contains(line[pos+run:], []byte("`"))) {
			fence, fenceLength = line[pos], run

			continue
		}

		lower := bytes.ToLower(line[pos:])
		if ends := htmlBlockEnds(lower); ends != nil {
			if !containsAny(lower, ends) {
				htmlEnds = ends
			}

			continue
		}

		if w > state.indent || line[pos] != '!' || run < 3 {
			continue
		}

		switch {
		case !bare:
			opened = run
		case len(waiting) > 0:
			if run >= waiting[len(waiting)-1] {
				waiting = waiting[:len(waiting)-1]
			}
		case run >= state.length:
			return true
		}
	}

	return false
}

// attributesEndLine reports whether an attribute block in goldmark v1's
// grammar starts at the reader's position, blanks aside, and runs to the end
// of its line.
func attributesEndLine(source []byte, at text.Segment) bool {
	end, _, ok := syntax.AttributeBlock(source, at.Start)

	return ok && util.IsBlank(source[end:max(end, at.Stop)])
}
