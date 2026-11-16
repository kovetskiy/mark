// Package admonitiongen writes small Markdown documents around MkDocs
// admonitions, for the tests that hold where the admonition parser reads
// closing runs to the blocks goldmark parses around them.
//
// Every word of a document's text is written once: "w1", "w2" and so on in
// its paragraphs, headings, code and HTML, "t1" and so on in its titles. A
// test can so tell text that was lost, published twice or moved.
package admonitiongen

import (
	"slices"
	"strconv"
	"strings"
)

// Rand is where a document's choices come from: a *math/rand.Rand, or the
// bytes a fuzz target was given.
type Rand interface {
	// Intn returns a number in [0, n).
	Intn(n int) int
}

// Bytes is a Rand that reads its choices off a byte string, and chooses 0
// once that has run out, which ends a document.
type Bytes []byte

// Intn implements Rand.
func (b *Bytes) Intn(n int) int {
	if len(*b) == 0 || n <= 1 {
		return 0
	}

	c := int((*b)[0])
	*b = (*b)[1:]

	return c % n
}

// Options says what a document may hold besides admonitions with indented
// bodies, paragraphs and headings.
type Options struct {
	// Marker is the character an admonition is opened and closed by a run
	// of, '!' when zero.
	Marker byte

	// Bodiless lets an admonition be written with no body.
	Bodiless bool

	// Unindented lets a body be level with its opening line and end at a
	// closing run, and Runless lets it go without the run.
	Unindented bool
	Runless    bool

	// Runs lets a line of the marker alone be written: after an indented
	// body, in code and HTML, and on its own.
	Runs bool

	// Literal lets fenced code and HTML blocks be written.
	Literal bool

	// Unclosed lets a fenced code block or an HTML block go without its
	// closing line.
	Unclosed bool

	// DivRuns lets a line of the marker alone be written inside an HTML
	// block that ends at a blank line, such as a <div>, where an admonition
	// was opened at the same level before it. Such a line closes that
	// admonition and cuts the block in two.
	DivRuns bool

	// Containers lets blocks be written in list items and blockquotes.
	Containers bool

	// Lazy lets a paragraph go on in a line without the indentation or the
	// markers of what it is in. Without LazyDeep it does so only where each
	// admonition the line falls out of holds the paragraph itself, or holds
	// the list item or the indented body the paragraph is in.
	Lazy     bool
	LazyDeep bool

	// Indents lets an opening line be indented one to three columns, as a
	// code fence or an HTML block always may be.
	Indents bool

	// Ragged lets a line be indented more than the opening line of an
	// admonition and less than its indented body, which reads as part of
	// that body: where the body starts deeper than the lines after it, or a
	// line after it is indented a little. It lets a line after a list item
	// be indented into the item too. Without it, such indentation is left
	// out wherever it would be read so.
	Ragged bool
}

// All is every option set.
func All() Options {
	return Options{
		Bodiless: true, Unindented: true, Runless: true, Runs: true, Literal: true, Unclosed: true, DivRuns: true,
		Containers: true, Lazy: true, LazyDeep: true, Indents: true, Ragged: true,
	}
}

// Document is a document as written.
type Document struct {
	Text string

	// Literal is the fenced code blocks and HTML blocks written with their
	// closing lines, but for the HTML blocks that end at a blank line: each
	// as the numbers of its opening and closing lines, counting from 0. A
	// line between the two is the block's text wherever the opening line
	// opens the block.
	Literal [][2]int
}

// New writes a document from r's choices. It uses tabs only for the odd
// indented body; Tabbed spells more of a document's blanks with them.
func New(r Rand, o Options) Document {
	if o.Marker == 0 {
		o.Marker = '!'
	}

	g := &generator{r: r, o: o, left: 30}
	g.blocks(&place{}, 2+r.Intn(4))

	if len(g.lines) == 0 {
		return Document{}
	}

	return Document{Text: strings.Join(g.lines, "\n") + "\n", Literal: g.literal}
}

type generator struct {
	r Rand
	o Options

	lines   []string
	literal [][2]int
	words   int

	// left is how many more lines the document may take.
	left int

	// text is where the last line was written, when it was a line of a
	// paragraph: a line of text straight after it goes on that paragraph,
	// wherever it is written.
	text *place
}

// place is where a block is written: what its lines start with.
type place struct {
	parent *place

	// lead starts every line here after what the parent's start with; first
	// starts the next one instead while pending, which is how a list item's
	// marker is written.
	lead    string
	first   string
	pending bool

	depth int

	// opened is whether an admonition was opened at this level, nested
	// whether one or a list item or blockquote was written here, and quote
	// whether the place is a blockquote.
	opened bool
	nested bool
	quote  bool

	// literal is whether a code or HTML block was written here or in a
	// place in this one.
	literal bool

	// exact is whether the next line here would be ragged if it were
	// indented any further: the first line of a body, and the line after an
	// indented one or after a list item.
	exact bool
}

// start returns what the next line here starts with, and takes the marker
// that was pending.
func (p *place) start() string {
	s := ""
	if p.parent != nil {
		s = p.parent.start()
	}

	if p.pending {
		p.pending = false

		return s + p.first
	}

	return s + p.lead
}

// rest returns what a line here starts with once any marker is written.
func (p *place) rest() string {
	if p.parent == nil {
		return p.lead
	}

	return p.parent.rest() + p.lead
}

// markerPending reports whether the next line here carries a list marker.
func (p *place) markerPending() bool {
	return p.pending || (p.parent != nil && p.parent.markerPending())
}

// indentNoFurther marks the next line, here or in a place around this one,
// as one that would be ragged if indented any further.
func (p *place) indentNoFurther() {
	for ; p != nil; p = p.parent {
		p.exact = true
	}
}

func (p *place) inside(lead string) *place {
	return &place{parent: p, lead: lead, depth: p.depth}
}

func (g *generator) line(p *place, text string) {
	start := p.start()

	// The blank after a quote marker is optional where text follows it.
	if strings.HasSuffix(start, "> ") && text != "" && text[0] != ' ' && text[0] != '\t' && g.r.Intn(6) == 0 {
		start = start[:len(start)-1]
	}

	g.lines = append(g.lines, start+text)
	g.left--
	g.text = nil

	for ; p != nil; p = p.parent {
		p.exact = false
	}
}

// indent returns the blanks to indent a block by, one of choices, or none
// where they would make a ragged line.
func (g *generator) indent(p *place, choices ...string) string {
	if p.markerPending() || (p.exact && !g.o.Ragged) {
		return ""
	}

	return g.pick(choices...)
}

func (g *generator) blank(p *place) {
	// A blank line cannot carry a list marker.
	if p.markerPending() {
		return
	}

	g.lines = append(g.lines, strings.TrimRight(p.rest(), " \t"))
	g.left--
	g.text = nil
}

func (g *generator) word() string {
	g.words++

	return "w" + strconv.Itoa(g.words)
}

func (g *generator) pick(choices ...string) string {
	return choices[g.r.Intn(len(choices))]
}

// closing writes a line of a run. One that closes nothing is a paragraph.
func (g *generator) closing(p *place, length int) {
	g.line(p, g.run(length))
	g.text = p
}

func (g *generator) run(length int) string {
	return strings.Repeat(string(g.o.Marker), length)
}

func (g *generator) blocks(p *place, count int) {
	for i := 0; i < count && g.left > 0; i++ {
		g.block(p)

		if g.r.Intn(3) != 0 {
			g.blank(p)
		}
	}
}

func (g *generator) block(p *place) {
	switch k := g.r.Intn(14); {
	case k < 3:
		g.paragraph(p)
	case k < 8:
		g.admonition(p)
	case k < 10 && g.o.Literal:
		if g.r.Intn(2) == 0 {
			g.fence(p)
		} else {
			g.html(p)
		}
	case k < 12 && g.o.Containers && p.depth < 3:
		g.container(p)
	case k == 12:
		g.line(p, g.pick("# ", "## ")+g.word())
	case k == 13 && g.o.Runs:
		g.closing(p, 3+g.r.Intn(2))
	default:
		g.paragraph(p)
	}
}

func (g *generator) paragraph(p *place) {
	// Straight after a line of a paragraph written elsewhere, this one
	// would go on that one lazily.
	if lazy := g.o.Lazy && (g.o.LazyDeep || g.text.near(p)); g.text != nil && g.text != p && !lazy {
		g.blank(p)
	}

	defer func() { g.text = p }()

	g.line(p, g.word()+" "+g.word())

	for i := g.r.Intn(3); i > 0 && g.left > 0; i-- {
		if shorter := g.shorter(p); g.o.Lazy && len(shorter) > 0 && g.r.Intn(2) == 0 {
			g.lines = append(g.lines, shorter[g.r.Intn(len(shorter))]+g.word())
			g.left--

			continue
		}

		g.line(p, g.word())
	}
}

// shorter returns what the lines of the places around p start with, where
// that is less than what p's do: a paragraph line starting so is a lazy one.
func (g *generator) shorter(p *place) []string {
	var found []string

	own := p.rest()
	for up := p.parent; up != nil; up = up.parent {
		if lead := up.rest(); lead != own && !slices.Contains(found, lead) && (g.o.LazyDeep || p.near(up)) {
			found = append(found, lead)
		}
	}

	return found
}

// near reports whether up is p or a place around it, and each place from p
// up to it where an admonition may be open has p in it directly, as a list
// item or an indented body with nothing in it but text, and no code or HTML
// block anywhere in it.
func (p *place) near(up *place) bool {
	for at := p; at != nil; at = at.parent {
		if at.opened && (at.literal || (at != p && (at != p.parent || p.quote || p.nested))) {
			return false
		}

		if at == up {
			return true
		}
	}

	return false
}

// holdsLiteral marks p and the places around it as holding a code or HTML
// block.
func (p *place) holdsLiteral() {
	for ; p != nil; p = p.parent {
		p.literal = true
	}
}

func (g *generator) admonition(p *place) {
	p.nested = true

	level := p
	if g.o.Indents {
		if indent := g.indent(p, "", "", "", "", "", " ", "  ", "   "); indent != "" {
			level = p.inside(indent)
		}
	}

	length := 3 + g.pickInt(0, 0, 0, 1, 2)

	opening := g.run(length) + " " + g.pick("note", "tip", "warning", "custom", "a")
	if g.r.Intn(3) != 0 {
		g.words++
		opening += ` "t` + strconv.Itoa(g.words) + `"`
	}

	g.line(level, opening)
	level.opened = true

	// A line indented under the opening line is its body, wherever it is
	// written, and the lines after it then have to be as deep.
	level.indentNoFurther()

	kind := g.r.Intn(6)
	if kind == 0 && !g.o.Bodiless {
		kind = 1
	}

	if kind == 0 {
		// No body: at most a closing run straight after.
		if g.o.Runs && g.r.Intn(4) == 0 {
			g.closing(level, length)
		}

		return
	}

	if kind == 5 {
		g.blank(level)
		kind = 1 + g.r.Intn(4)
	}

	if kind > 2 && g.o.Unindented {
		g.body(level)

		switch {
		case !g.o.Runless:
			// A run straight after an indented body closes the admonition
			// that body belongs to, and a line of text there would go on
			// its last paragraph.
			if level.exact {
				g.line(level, "## "+g.word())
			}

			g.closing(level, length+g.pickInt(0, 0, 1))
		case g.r.Intn(4) != 0:
			g.closing(level, max(3, length+g.pickInt(0, 0, 0, 1, -1)))
		}

		return
	}

	body := level.inside(g.pick("    ", "    ", "  ", "   ", "\t"))
	body.exact = true
	g.body(body)
	level.indentNoFurther()

	if g.o.Runs && g.r.Intn(3) == 0 {
		g.closing(level, max(3, length+g.pickInt(0, 0, 0, 1, -1)))
	}
}

// body writes the blocks of an admonition's body, and a line of text where
// the document has no room for them and an admonition has to have a body.
func (g *generator) body(p *place) {
	before := len(g.lines)
	g.blocks(p, 1+g.r.Intn(3))

	if len(g.lines) == before && !g.o.Bodiless {
		g.line(p, g.word())
	}
}

func (g *generator) pickInt(choices ...int) int {
	return choices[g.r.Intn(len(choices))]
}

func (g *generator) fence(p *place) {
	p.holdsLiteral()

	indent := g.indent(p, "", "", "", " ", "  ", "   ")
	char := g.pick("`", "~")
	fence := strings.Repeat(char, 3+g.r.Intn(3))

	// A shorter fence and one of the other character do not close it.
	other := strings.Repeat(string("`~"[strings.Index("~`", char)]), 3+g.r.Intn(3))

	opening := len(g.lines)
	g.line(p, indent+fence+g.pick("", "", "text"))

	for i := 1 + g.r.Intn(3); i > 0; i-- {
		g.literalLine(p, indent, true, fence[1:], other)
	}

	if g.o.Unclosed && g.r.Intn(8) == 0 {
		return
	}

	g.literal = append(g.literal, [2]int{opening, len(g.lines)})
	g.line(p, g.pick(indent, indent, "")+fence+g.pick("", "", fence[:1]))
}

// literalLine writes a line of a code or HTML block: a word, a line that
// would open an admonition anywhere else, or with runs one that would close
// an admonition. It may write one of others first, a line that would close
// another kind of block, so that the run is read as text only by a reader
// that knows which kind this one is.
func (g *generator) literalLine(p *place, indent string, runs bool, others ...string) {
	if !g.o.Runs {
		g.line(p, indent+g.word())

		return
	}

	k := g.r.Intn(6)
	if k == 2 && len(others) > 0 {
		if other := g.pick(others...); len(other) > 2 || other == ">" {
			g.line(p, indent+other)

			k = 0
		}
	}

	switch {
	case k == 0 && runs:
		g.line(p, indent+g.run(3+g.r.Intn(3)))
	case k == 1:
		g.line(p, indent+g.run(3)+" note")
	default:
		g.line(p, indent+g.word())
	}
}

// htmlBlocks are the HTML blocks written, by their opening and closing
// lines, and the lines that close another of them and not this one, in XML
// as in Markdown; the last two end at a blank line whether closed or not.
var htmlBlocks = [][]string{
	{"<pre>", "</pre>", "-->", "?>", ">"},
	{"<script>", "</script>", "-->", "?>", ">"},
	{"<style>", "</style>", "-->", "?>", ">"},
	{"<textarea>", "</textarea>", "-->", "?>", ">"},
	{"<!--", "-->", "?>", "]]>", ">", "</pre>"},
	{"<![CDATA[", "]]>", "-->", "?>", ">", "</pre>"},
	{"<!DOCTYPE html", ">"},
	{"<?php", "?>", "-->", "]]>", ">", "</pre>"},
	{"<div>", "</div>"},
	{"<details>", "</details>"},
}

func (g *generator) html(p *place) {
	p.holdsLiteral()

	kind := g.r.Intn(len(htmlBlocks))
	block := htmlBlocks[kind]
	untilBlank := kind >= len(htmlBlocks)-2

	indent := g.indent(p, "", "", "", " ", "  ", "   ")
	runs := !untilBlank || g.o.DivRuns || !p.opened

	opening := len(g.lines)
	g.line(p, indent+block[0])

	for i := 1 + g.r.Intn(2); i > 0; i-- {
		g.literalLine(p, indent, runs, block[2:]...)
	}

	switch {
	case untilBlank:
		g.line(p, indent+block[1])

		if g.o.Runs && g.r.Intn(4) == 0 {
			g.closing(p, 3)
		}

		g.blank(p)
	case !g.o.Unclosed || g.r.Intn(8) != 0:
		g.literal = append(g.literal, [2]int{opening, len(g.lines)})
		g.line(p, indent+block[1])
	}
}

func (g *generator) container(p *place) {
	p.nested = true

	if g.r.Intn(2) == 0 {
		// Straight after another, a blockquote would be the same one.
		if n := len(g.lines); n > 0 && strings.HasPrefix(g.lines[n-1], p.rest()+">") {
			g.blank(p)
		}

		quote := p.inside("> ")
		quote.quote = true
		quote.depth++
		g.blocks(quote, 1+g.r.Intn(3))

		return
	}

	marker := g.pick("- ", "- ", "* ", "+ ", "1. ", "7) ", "-   ")

	// A list numbered from anything but 1 cannot interrupt a paragraph: its
	// first line would be text of that paragraph, and an HTML block opened
	// on it would be closed outside the paragraph.
	if marker == "7) " && g.text != nil {
		g.blank(p)
	}

	for i := 1 + g.r.Intn(2); i > 0 && g.left > 0; i-- {
		item := p.inside(strings.Repeat(" ", len(marker)))
		item.depth++
		item.first, item.pending = marker, true

		g.blocks(item, 1+g.r.Intn(2))
	}

	// A line indented after a list item is in it.
	p.indentNoFurther()
}

// Tabbed returns d with some of the blanks its lines start with written as
// tabs that span the same columns, tab stops being four columns apart.
// CommonMark reads the two alike.
func (d Document) Tabbed(r Rand) Document {
	lines := strings.Split(d.Text, "\n")
	for i, line := range lines {
		lines[i] = tabbed(r, line)
	}

	return Document{Text: strings.Join(lines, "\n"), Literal: d.Literal}
}

func tabbed(r Rand, line string) string {
	var out strings.Builder

	column := 0
	at := 0

	for at < len(line) {
		switch c := line[at]; c {
		case ' ':
			end := at
			for end < len(line) && line[end] == ' ' {
				end++
			}

			// Tabs for the blanks up to the last tab stop they reach.
			to := column + end - at
			if stop := to / 4 * 4; stop > column && r.Intn(2) == 0 {
				for ; column < stop; column = column/4*4 + 4 {
					out.WriteByte('\t')
				}

				out.WriteString(strings.Repeat(" ", to-stop))
				column, at = to, end

				continue
			}

			out.WriteString(line[at:end])
			column += end - at
			at = end
		case '\t':
			out.WriteByte(c)
			column = column/4*4 + 4
			at++
		case '>':
			out.WriteByte(c)
			column++
			at++
		default:
			if n := listMarker(line[at:]); n > 0 {
				// goldmark misplaces the content of a list item whose marker
				// is followed by a tab anywhere but at the left margin, so
				// the blanks after any other marker stay blanks.
				if at > 0 {
					for n < len(line)-at && line[at+n] == ' ' {
						n++
					}
				}

				out.WriteString(line[at : at+n])
				column += n
				at += n

				continue
			}

			out.WriteString(line[at:])

			return out.String()
		}
	}

	return out.String()
}

// listMarker returns the length of the list marker line starts with, without
// the blanks after it, and 0 when it starts with none.
func listMarker(line string) int {
	n := 0
	for n < len(line) && line[n] >= '0' && line[n] <= '9' {
		n++
	}

	switch {
	case n > 0 && n < len(line) && (line[n] == '.' || line[n] == ')'):
		n++
	case n == 0 && len(line) > 0 && strings.IndexByte("-+*", line[0]) >= 0:
		n = 1
	default:
		return 0
	}

	if n < len(line) && line[n] == ' ' {
		return n
	}

	return 0
}
