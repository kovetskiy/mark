package parser

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// NewConfluenceTagParser returns an inline parser that parses <ac:* /> and <ri:* /> tags to ensure that Confluence specific tags are parsed
// as ast.KindRawHtml so they are not escaped at render time. The parser must be registered with a higher priority
// than goldmark's linkParser. Otherwise, the linkParser would parse the <ac:* /> tags.
func NewConfluenceTagParser() parser.InlineParser {
	return &confluenceTagParser{}
}

var _ parser.InlineParser = (*confluenceTagParser)(nil)

// confluenceTagParser is a stripped down version of goldmark's rawHTMLParser.
// See: https://github.com/yuin/goldmark/blob/master/parser/raw_html.go
type confluenceTagParser struct {
}

func (s *confluenceTagParser) Trigger() []byte {
	return []byte{'<'}
}

func (s *confluenceTagParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) > 1 && util.IsAlphaNumeric(line[1]) {
		node := s.parseMultiLineRegexp(openTagRegexp, block, pc)
		if node == nil {
			return nil
		}

		tag := node.(*ast.RawHTML).Value.Bytes(block.Source())
		if name := literalElementName(tag); name != nil {
			if closer := s.parseLiteralContent(parent, node, name, block); closer != nil {
				return closer
			}
		}

		return node
	}
	if len(line) > 2 && line[1] == '/' && util.IsAlphaNumeric(line[2]) {
		return s.parseMultiLineRegexp(closeTagRegexp, block, pc)
	}
	if len(line) > 2 && line[1] == '!' && line[2] >= 'A' && line[2] <= 'Z' {
		return s.parseUntil(block, closeDecl, pc)
	}
	if bytes.HasPrefix(line, openCDATA) {
		return s.parseUntil(block, closeCDATA, pc)
	}
	return nil
}

var tagnamePattern = `([A-Za-z][A-Za-z0-9-]*)`

var spaceOrOneNewline = `(?:[ \t]|(?:\r\n|\n){0,1})`
var attributePattern = `(?:[\r\n \t]+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:[\r\n \t]*=[\r\n \t]*(?:[^\"'=<>` + "`" + `\x00-\x20]+|'[^']*'|"[^"]*"))?)`

// Only match <ac:*/> and <ri:*/> tags
var openTagRegexp = regexp.MustCompile("^<(ac|ri):" + tagnamePattern + attributePattern + `*` + spaceOrOneNewline + `*/?>`)

// Both prefixes, matching openTagRegexp above: covering only ac: meant a
// separate </ri:page> closing tag fell through to the text renderer and was
// entity-escaped, breaking the element it was meant to close.
var closeTagRegexp = regexp.MustCompile("^</(ac|ri):" + tagnamePattern + spaceOrOneNewline + `*>`)

var openCDATA = []byte("<![CDATA[")
var closeCDATA = []byte("]]>")
var closeDecl = []byte(">")

func (s *confluenceTagParser) parseUntil(block text.Reader, closer []byte, _ parser.Context) ast.Node {
	savedLine, savedSegment := block.Position()
	// Raw markup, so bound to the identity decoder: an entity in it is the
	// author's and goes out as written.
	var value text.ValueBuilder
	value.Decoder(text.IdentityDecoder)
	for {
		line, segment := block.PeekLine()
		if line == nil {
			break
		}
		index := bytes.Index(line, closer)
		if index > -1 {
			value.AddSegment(segment.WithStop(segment.Start + index + len(closer)))
			block.Advance(index + len(closer))
			return ast.NewRawHTML(value.BuildMultiLine())
		}
		value.AddSegment(segment)
		block.AdvanceLine()
	}
	block.SetPosition(savedLine, savedSegment)
	return nil
}

func (s *confluenceTagParser) parseMultiLineRegexp(reg *regexp.Regexp, block text.Reader, _ parser.Context) ast.Node {
	sline, ssegment := block.Position()
	if block.Match(reg) {
		var value text.ValueBuilder
		value.Decoder(text.IdentityDecoder)
		eline, esegment := block.Position()
		block.SetPosition(sline, ssegment)
		for {
			line, segment := block.PeekLine()
			if line == nil {
				break
			}
			l, _ := block.Position()
			start := segment.Start
			if l == sline {
				start = ssegment.Start
			}
			end := segment.Stop
			if l == eline {
				end = esegment.Start
			}

			value.AddIndex(text.NewIndex(start, end))
			if l == eline {
				block.Advance(end - start)
				break
			} else {
				block.AdvanceLine()
			}
		}
		return ast.NewRawHTML(value.BuildMultiLine())
	}
	return nil
}

// literalElements hold a string Confluence reads as it stands -- a pattern, a
// query, a URL, an id -- so what is written between their tags is not
// Markdown. A macro's ac:rich-text-body is, and is not among them.
var literalElementRegexp = regexp.MustCompile(`^<(ac:(?:parameter|default-parameter|task-id|task-status))[\s>]`)

// literalElementName returns the name of the literal element tag opens, or
// nil when tag opens none: another element, or one that closes itself.
func literalElementName(tag []byte) []byte {
	if bytes.HasSuffix(tag, []byte("/>")) {
		return nil
	}

	if groups := literalElementRegexp.FindSubmatch(tag); groups != nil {
		return groups[1]
	}

	return nil
}

// parseLiteralContent reads on from the opening tag of a literal element to
// its closing tag, and gives the text between them to parent as text rather
// than leaving it to the inline parsers. It returns the closing tag, with
// open and the content already appended to parent before it, one node to a
// tag as they would be without it, since that is the shape the transformers
// and the paragraph renderer read.
//
// Without a closing tag in this block it returns nil and appends nothing, and
// the tag is an opening tag like any other.
func (s *confluenceTagParser) parseLiteralContent(parent, open ast.Node, name []byte, block text.Reader) ast.Node {
	savedLine, savedSegment := block.Position()

	// Most elements close on the line they open on, which is read where it
	// stands; only one that does not has the rest of the block gathered.
	line, _ := block.PeekLine()
	pieces, consumed := literalContent(line, name)
	if pieces == nil {
		var rest []byte
		for {
			line, _ := block.PeekLine()
			if line == nil {
				break
			}
			rest = append(rest, line...)
			block.AdvanceLine()
		}
		block.SetPosition(savedLine, savedSegment)

		pieces, consumed = literalContent(rest, name)
	}
	if pieces == nil {
		return nil
	}

	for consumed > 0 {
		line, _ := block.PeekLine()
		if consumed < len(line) {
			block.Advance(consumed)
			break
		}
		consumed -= len(line)
		block.AdvanceLine()
	}

	parent.AppendChild(open)
	for _, piece := range pieces[:len(pieces)-1] {
		parent.AppendChild(rawHTML(piece, savedSegment.Start))
	}

	return rawHTML(pieces[len(pieces)-1], savedSegment.Start)
}

// rawHTML is a node of markup that is not a range of the source.
func rawHTML(markup []byte, pos int) ast.Node {
	node := ast.NewRawHTML(text.NewMultiLineValueFromString(string(markup), text.IdentityDecoder))
	node.SetPos(pos)

	return node
}

// literalContent splits what follows the opening tag of the literal element
// called name into the markup of each node up to and including its closing
// tag, and reports how many bytes of rest that took. It returns nil when rest
// does not close the element.
//
// A Confluence tag, a CDATA section and a comment are each a piece as
// written: a parameter may hold an <ri:url/> or an <ac:link>. Everything else
// is text, escaped.
func literalContent(rest []byte, name []byte) (pieces [][]byte, consumed int) {
	textStart, depth := 0, 1

	flush := func(end int) {
		if end > textStart {
			pieces = append(pieces, escapeLiteral(rest[textStart:end]))
		}
	}

	for i := 0; i < len(rest); {
		if rest[i] != '<' {
			i++
			continue
		}

		tail := rest[i:]
		length := 0

		switch {
		case bytes.HasPrefix(tail, openCDATA):
			length = sectionLength(tail, closeCDATA)
		case bytes.HasPrefix(tail, openComment):
			length = sectionLength(tail, closeComment)
		default:
			if groups := closeTagRegexp.FindSubmatch(tail); groups != nil {
				length = len(groups[0])
				if sameElement(groups, name) {
					depth--
				}
			} else if groups := openTagRegexp.FindSubmatch(tail); groups != nil {
				length = len(groups[0])
				if sameElement(groups, name) && !bytes.HasSuffix(groups[0], []byte("/>")) {
					depth++
				}
			}
		}

		if length == 0 {
			// A "<" that opens nothing: text.
			i++
			continue
		}

		flush(i)
		pieces = append(pieces, tail[:length])
		i += length
		textStart = i

		if depth == 0 {
			return pieces, i
		}
	}

	return nil, 0
}

var openComment = []byte("<!--")
var closeComment = []byte("-->")

// sectionLength is the length of the section at the start of b that closer
// ends, or 0 when it is not closed.
func sectionLength(b, closer []byte) int {
	if end := bytes.Index(b, closer); end >= 0 {
		return end + len(closer)
	}

	return 0
}

// sameElement reports whether a match of openTagRegexp or closeTagRegexp is a
// tag of the element called name.
func sameElement(groups [][]byte, name []byte) bool {
	return len(name) == len(groups[1])+1+len(groups[2]) &&
		bytes.HasPrefix(name, groups[1]) && bytes.HasSuffix(name, groups[2])
}

// entityDecoder resolves character references the way goldmark does for the
// text of a paragraph.
var entityDecoder = text.NewDecoder()

// escapeLiteral is b as markup for the text it spells: a character reference
// is the character it names, as it is in any text, and nothing else has a
// meaning -- a backslash stays a backslash, which doubling it tells the
// decoder.
func escapeLiteral(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte(`\`), []byte(`\\`))

	return util.EscapeHTML(entityDecoder.Decode(b))
}
