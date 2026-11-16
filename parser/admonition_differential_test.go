package parser_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/admonitiongen"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// An admonition whose body is level with its opening line ends at that line,
// and waits in the block it opened in for a closing run, which gives it the
// blocks goldmark parsed there in between. The tests here write documents out
// of the pieces that decide where a run is read -- code and HTML blocks, list
// items, blockquotes, lazy lines, tabs -- and require of every admonition that
// it holds what that run gave it or nothing, that no run is read inside code or
// HTML, that no run it waits for is left as text, and that no text is lost or
// moved.

// differentialSeed and differentialDocuments fix the documents the seeded
// test reads, so that a failure names a document anyone can read again: the
// one admonitiongen.New writes from rand.New(rand.NewSource(seed)).
const (
	differentialSeed      = 20240611
	differentialDocuments = 2000
)

var admonitionBlocks = parser.New(
	parser.WithBlockParsers(
		util.Prioritized(cparser.NewAdmonitionParser(), 100),
		util.Prioritized(cparser.NewAdmonitionHTMLBlockParser(), 899),
	),
)

// reading is a document as the admonition parser read it.
type reading struct {
	source []byte
	doc    ast.Node

	// work is how many waiting admonitions the parse looked at.
	work int
}

func readAdmonitions(source string) reading {
	pc := parser.NewContext()
	src := []byte(source)

	doc := admonitionBlocks.Parse(src, parser.WithContext(pc))

	return reading{source: src, doc: doc, work: cparser.WaitingWorkOf(pc)}
}

var generatedWord = regexp.MustCompile(`\b[wt][0-9]+\b`)

// text returns what a block holds itself, its children's text aside.
func (r reading) text(node ast.Node) string {
	switch n := node.(type) {
	case *cparser.Admonition:
		return string(n.Title)
	case *ast.CodeBlock:
		return n.Value.Str(r.source)
	case *ast.HTMLBlock:
		return n.Value.Str(r.source)
	case ast.BlockNode:
		var text strings.Builder
		for _, segment := range n.Source() {
			text.Write(segment.Bytes(r.source))
		}

		return text.String()
	}

	return ""
}

// tree returns the blocks in document order, one a line and indented by
// depth: each as its kind and the generated words it holds, so that two
// spellings of the same blanks read alike.
func (r reading) tree() []string {
	var lines []string

	var walk func(node ast.Node, depth string)
	walk = func(node ast.Node, depth string) {
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if _, ok := child.(ast.BlockNode); !ok {
				continue
			}

			line := depth + child.Kind().String()
			if admonition, ok := child.(*cparser.Admonition); ok {
				line += "!" + string(admonition.AdmonitionClass)
			}

			lines = append(lines, line+" "+strings.Join(generatedWord.FindAllString(r.text(child), -1), " "))
			walk(child, depth+"  ")
		}
	}
	walk(r.doc, "")

	return lines
}

// words returns the generated words in the order the tree holds them.
func (r reading) words() []string {
	var words []string

	_ = ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if _, ok := node.(ast.BlockNode); entering && ok && node.Kind() != ast.KindDocument {
			words = append(words, generatedWord.FindAllString(r.text(node), -1)...)
		}

		return ast.WalkContinue, nil
	})

	return words
}

// closedAsRead holds each admonition to how its body was read: one that
// ended at its opening line, a line level with it after that, and that no run
// closed holds nothing, and one a run closed holds the blocks before the run's
// line, and the block after it starts after that line. The run's line is a
// run, as long as the opening one or longer, and nothing else.
func (r reading) closedAsRead() error {
	return ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		admonition, ok := node.(*cparser.Admonition)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}

		body := cparser.BodyOf(admonition)
		line := r.lineOf(admonition.Pos()) + 1

		switch {
		case body.ClosedAt == 0 && body.Level && admonition.HasChildren():
			return ast.WalkStop, fmt.Errorf("line %d: no run closed the admonition, and it holds a block", line)
		case body.ClosedAt == 0:
			return ast.WalkContinue, nil
		}

		run := r.lineOf(body.ClosedAt)
		text := strings.TrimRight(r.lineText(run), " \t\n")
		if n := len(text) - len(strings.TrimRight(text, "!")); n < body.Length {
			return ast.WalkStop, fmt.Errorf("line %d: closed by line %d, %q, which is no run as long as its own",
				line, run+1, text)
		}

		if last := lastLine(r, admonition); last >= run {
			return ast.WalkStop, fmt.Errorf("line %d: closed by line %d, and holds line %d", line, run+1, last+1)
		}

		for next := node; next != nil; next = next.Parent() {
			if sibling := next.NextSibling(); sibling != nil {
				if first := firstLine(r, sibling); first >= 0 && first <= run {
					return ast.WalkStop, fmt.Errorf("line %d: closed by line %d, and the block after it starts on line %d",
						line, run+1, first+1)
				}

				break
			}
		}

		return ast.WalkContinue, nil
	})
}

// lineText returns line n of the source, counting from 0.
func (r reading) lineText(n int) string {
	return strings.Split(string(r.source), "\n")[n]
}

// lines returns the first and last lines of node and what it holds, -1 for a
// node that holds no line.
func lines(r reading, node ast.Node) (first, last int) {
	first, last = -1, -1

	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		var segments []int
		switch b := n.(type) {
		case *ast.CodeBlock:
			for _, segment := range b.Value.Segments() {
				segments = append(segments, segment.Start)
			}
		case *ast.HTMLBlock:
			for _, segment := range b.Value.Segments() {
				segments = append(segments, segment.Start)
			}
		case ast.BlockNode:
			for _, segment := range b.Source() {
				segments = append(segments, segment.Start)
			}
		}

		if _, ok := n.(*cparser.Admonition); ok {
			segments = append(segments, n.Pos())
		}

		for _, at := range segments {
			l := r.lineOf(at)
			if first < 0 || l < first {
				first = l
			}
			last = max(last, l)
		}

		return ast.WalkContinue, nil
	})

	return first, last
}

func firstLine(r reading, node ast.Node) int {
	first, _ := lines(r, node)
	return first
}

func lastLine(r reading, node ast.Node) int {
	_, last := lines(r, node)
	return last
}

// lineOf returns the number of the line that holds byte at, counting from 0.
func (r reading) lineOf(at int) int {
	return bytes.Count(r.source[:at], []byte("\n"))
}

// column returns the column of byte at in its line, a tab reaching to the
// next of the tab stops four columns apart.
func (r reading) column(at int) int {
	column := 0
	for _, c := range r.source[strings.LastIndexByte(string(r.source[:at]), '\n')+1 : at] {
		if c == '\t' {
			column = column/4*4 + 4
		} else {
			column++
		}
	}

	return column
}

// closedInLiteralText returns an error for an admonition a line inside a
// fenced code block or an HTML block closed, cutting the block in two: one
// opened before a block the document was written with, where the tree has
// that block, and closed by a line between the block's opening and closing
// lines.
func (r reading) closedInLiteralText(literal [][2]int) error {
	blocks := map[int]bool{}

	// closed is the lines admonitions were opened on and closed by.
	var closed [][2]int

	_ = ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.CodeBlock:
			if n.CodeBlockKind == ast.CodeBlockKindFenced {
				blocks[r.lineOf(n.Pos())] = true
			}
		case *ast.HTMLBlock:
			blocks[r.lineOf(n.Pos())] = true
		case *cparser.Admonition:
			if at := cparser.BodyOf(n).ClosedAt; at > 0 {
				closed = append(closed, [2]int{r.lineOf(n.Pos()), r.lineOf(at)})
			}
		}

		return ast.WalkContinue, nil
	})

	for _, admonition := range closed {
		for _, block := range literal {
			if admonition[0] < block[0] && block[0] < admonition[1] && admonition[1] < block[1] && blocks[block[0]] {
				return fmt.Errorf("line %d closed the admonition opened on line %d, and is text of the block that opens on line %d",
					admonition[1]+1, admonition[0]+1, block[0]+1)
			}
		}
	}

	return nil
}

// runLeftAsText returns an error for an admonition that waited for a closing
// run and was closed by none while one for it lay ahead: a paragraph, or an
// HTML block that ends at a blank line, after it in the same list item,
// blockquote or body, with a line of nothing but a run as long as its own in
// its column.
//
// It looks only at an admonition that is not indented in what holds it, whose
// column is that of the lines of a paragraph after it.
func (r reading) runLeftAsText(marker byte) error {
	run := func(at int) int {
		n := 0
		for at+n < len(r.source) && r.source[at+n] == marker {
			n++
		}

		return n
	}

	return ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		admonition, ok := node.(*cparser.Admonition)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}

		if body := cparser.BodyOf(admonition); !body.Level || body.ClosedAt > 0 || body.Indent != 0 {
			return ast.WalkContinue, nil
		}

		opening := admonition.Pos()
		column, length := r.column(opening), run(opening)

		for sibling := node.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
			var segments []text.Segment

			switch block := sibling.(type) {
			case *ast.Paragraph:
				segments = block.Source()
			case *ast.HTMLBlock:
				// A block like <div> ends at a run as a paragraph does.
				if block.HTMLBlockKind == ast.HTMLBlockKind6 || block.HTMLBlockKind == ast.HTMLBlockKind7 {
					segments = block.Value.Segments()
				}
			}

			for _, segment := range segments {
				segment = segment.TrimLeftSpace(r.source)
				n := run(segment.Start)
				if n < length || strings.TrimSpace(string(r.source[segment.Start+n:segment.Stop])) != "" {
					continue
				}

				if r.column(segment.Start) == column {
					return ast.WalkStop, fmt.Errorf(
						"line %d: the admonition ended at its opening line, and the run on line %d would have closed it",
						r.lineOf(opening)+1, r.lineOf(segment.Start)+1)
				}
			}
		}

		return ast.WalkContinue, nil
	})
}

// regular is the documents in which a closing run left as text is the
// parser's mistake. In the others a column says less about which block a line
// is in: an admonition indented in the body of another, or a line indented
// between an opening line and its body, shift the lines after them; and a code
// or HTML block left unclosed ends with the admonition it is in and not
// otherwise.
func regular() admonitiongen.Options {
	o := admonitiongen.All()
	o.Indents, o.Ragged, o.Unclosed, o.LazyDeep = false, false, false, false

	return o
}

// checkAdmonitionDocument reads a generated document and returns what is
// wrong with the reading: an admonition holding what its run did not give it,
// a code or HTML block cut in two, text lost, repeated or out of order, or, in a
// regular document, a closing run left as text.
func checkAdmonitionDocument(document admonitiongen.Document, regular bool) error {
	r := readAdmonitions(document.Text)

	if err := r.closedAsRead(); err != nil {
		return err
	}

	if err := r.closedInLiteralText(document.Literal); err != nil {
		return err
	}

	if regular {
		if err := r.runLeftAsText('!'); err != nil {
			return err
		}
	}

	if want, got := generatedWord.FindAllString(document.Text, -1), r.words(); !slices.Equal(want, got) {
		return fmt.Errorf("the document's words are %v and the tree's are %v", want, got)
	}

	return nil
}

// checkTabbedAlike reads a document with some of its blanks written as tabs
// and requires the tree the spaces give.
func checkTabbedAlike(r admonitiongen.Rand, document admonitiongen.Document, regular bool) error {
	tabbed := document.Tabbed(r)
	if tabbed.Text == document.Text {
		return nil
	}

	if err := checkAdmonitionDocument(tabbed, regular); err != nil {
		return fmt.Errorf("with tabs, %q: %w", tabbed.Text, err)
	}

	if want, got := readAdmonitions(document.Text).tree(), readAdmonitions(tabbed.Text).tree(); !slices.Equal(want, got) {
		return fmt.Errorf("with tabs, %q, the tree is\n%s\nand with spaces\n%s",
			tabbed.Text, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	return nil
}

// checkGenerated writes a document from r's choices, any document or a
// regular one, and checks it as written and with tabs.
func checkGenerated(r admonitiongen.Rand, regularOnly bool) error {
	o := admonitiongen.All()
	if regularOnly {
		o = regular()
	}

	document := admonitiongen.New(r, o)

	if err := checkAdmonitionDocument(document, regularOnly); err != nil {
		return fmt.Errorf("document %q: %w", document.Text, err)
	}

	if err := checkTabbedAlike(r, document, regularOnly); err != nil {
		return fmt.Errorf("document %q: %w", document.Text, err)
	}

	return nil
}

// TestAdmonitionGeneratedDocuments reads generated documents and requires of
// each that every admonition holds what its closing run gave it, or nothing,
// that none ends inside a code or HTML block, that no text is lost,
// repeated or moved, and that tabs read as the blanks they span; and of the
// regular ones, that none ends before a run that would have closed it.
func TestAdmonitionGeneratedDocuments(t *testing.T) {
	for n := range differentialDocuments {
		seed := int64(differentialSeed + n)

		if err := checkGenerated(rand.New(rand.NewSource(seed)), n%2 == 1); err != nil {
			t.Fatalf("seed %d, regular %v, %v", seed, n%2 == 1, err)
		}
	}
}

// FuzzAdmonitionDocuments is TestAdmonitionGeneratedDocuments for documents
// written from the fuzzer's bytes, which are also read as a document
// themselves: every admonition has to hold what it was read to hold on any
// input.
func FuzzAdmonitionDocuments(f *testing.F) {
	for _, seed := range []string{
		"",
		"!!! note \"T\"\nbody\n ```\n!!!\n ```\nmore\n!!!\nafter\n",
		">\t!!! note\n>\tbody\n>   !!!\n\nafter\n",
		"!!!! quote \"T\"\nw2\n\n!!! note \"N\"\n    w5\n\n!!!!\nafter\n",
		"- !!! note\n  text\ntext\n  !!!\n",
		"\x04\x09\x03\x01\x02\x05\x08\x00\x07\x06\x0b\x0a\x0d\x0c\x04\x04\x09\x09\x03\x03\x08\x08",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := readAdmonitions(string(data)).closedAsRead(); err != nil {
			t.Fatalf("document %q: %v", data, err)
		}

		for _, regularOnly := range []bool{false, true} {
			choices := admonitiongen.Bytes(data)
			if err := checkGenerated(&choices, regularOnly); err != nil {
				t.Fatalf("regular %v, %v", regularOnly, err)
			}
		}
	})
}

// TestAdmonitionsReadAsBefore holds the forms this parser has not changed
// the reading of to the reading they had: testdata/admonition-master.json
// holds generated documents with the tree the parser gave them before a body
// level with its opening line had to end at a closing run. They are of three
// classes. In "indented" every admonition has an indented body and no line
// of a closing run is written; in "closed" a body may be level with its
// opening line, and then ends at a closing run of its own; "lazy" is "closed"
// with paragraphs that go on in lines lacking the indentation or the markers
// of what they are in. Some of each are written with tabs.
//
// The trees are recorded rather than derived, because the reading they hold
// is what documents were written against. The file is made once: the
// documents by admonitiongen, each tree by parsing its document with the
// parser of the commit the change was made on. Of the lazy ones, those are
// left out where a closing run follows lines level with an indented body
// inside the admonition: that run closes the admonition, where the recorded
// reading has it close the body inside and leave the admonition open to the
// end.
func TestAdmonitionsReadAsBefore(t *testing.T) {
	data, err := os.ReadFile("testdata/admonition-master.json")
	require.NoError(t, err)

	var documents []struct {
		Class  string   `json:"class"`
		Source string   `json:"source"`
		Blocks []string `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal(data, &documents))
	require.NotEmpty(t, documents)

	for n, document := range documents {
		r := readAdmonitions(document.Source)

		require.NoError(t, r.closedAsRead(), "document %d, %s: %q", n, document.Class, document.Source)
		require.Equal(t, document.Blocks, r.tree(), "document %d, %s: %q", n, document.Class, document.Source)
	}
}

// TestAdmonitionClosingRunsAreReadInLinearTime: a page of admonitions that
// end at their opening lines waits for closing runs in one pass, whatever
// keeps each from the run at its end: the admonition after it, a run too
// short for it, or a code or HTML block the run is text of. The admonitions
// looked at are counted, since a clock says as much about the machine as
// about the parser.
func TestAdmonitionClosingRunsAreReadInLinearTime(t *testing.T) {
	const count = 2000

	for name, document := range map[string]string{
		"a run at the end":        strings.Repeat("!!! note \"T\"\n\ntext\n\n", count) + "!!!\n",
		"a run in code":           strings.Repeat("!!! note \"T\"\n\ntext\n\n", count) + "```\n!!!\n```\n",
		"one straight after one":  strings.Repeat("!!! a\ntext\n", count) + "!!!\n",
		"longer and shorter runs": strings.Repeat("!!! a\ntext\n!!!! a\ntext\n", count) + "!!!\n",
		"runs too short for each": strings.Repeat("!!!! a\ntext\n", count) + strings.Repeat("!!!\n", count),
		"each closed":             strings.Repeat("!!! a\ntext\n!!!\n", count),
		"each in a list item":     strings.Repeat("- !!! a\n  text\n", count) + "- !!!\n",
		"each with a run of its own and one around them": "!!!! a\n" +
			strings.Repeat("!!! b\n    text\n!!!\n", count) + "!!!!\n",

		// The comment and the fence are text of the block the <div> opens,
		// which ends at the blank line.
		"an unclosed comment in a div": strings.Repeat("!!! a\ntext\n<div>\n<!--\n</div>\n\n", count) + "!!!\n",
		"a code fence in a div":        strings.Repeat("!!! a\ntext\n<div>\n```\n</div>\n\n", count) + "!!!\n",
		"a run in each div":            strings.Repeat("!!! a\ntext\n<div>\n!!!\n", count),
		"an unclosed comment after a tag": strings.Repeat("!!! a\n> # h\n<span>\n<!--\n</span>\n\n", count) +
			"!!!\n",
		"an unclosed fence after a tag": strings.Repeat("!!! a\n> # h\n<span>\n~~~~\n</span>\n\n", count) +
			"```\n!!!\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := readAdmonitions(document)

			require.NoError(t, r.closedAsRead())
			assert.LessOrEqual(t, r.work, 2*strings.Count(document, "\n"))
		})
	}
}
