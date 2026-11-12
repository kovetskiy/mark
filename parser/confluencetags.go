package parser

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/yuin/goldmark/v2/ast"
	east "github.com/yuin/goldmark/v2/extension/ast"
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
		if name := literalElementName(tag); name != "" {
			if closer := s.parseLiteralContent(parent, node, name, block, pc); closer != nil {
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

// literalElementName returns the name of the literal element tag opens, or
// "" when tag opens none: another element, or one that closes itself.
func literalElementName(tag []byte) string {
	if bytes.HasSuffix(tag, []byte("/>")) {
		return ""
	}

	name := string(tag[1:])
	if end := strings.IndexAny(name, " \t\r\n>"); end >= 0 {
		name = name[:end]
	}
	if syntax.IsLiteralElement(name) {
		return name
	}

	return ""
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
func (s *confluenceTagParser) parseLiteralContent(
	parent, open ast.Node, name string, block text.Reader, pc parser.Context,
) ast.Node {
	unclosed, _ := pc.Get(unclosedLiteralsKey).(*unclosedLiterals)
	if unclosed == nil || unclosed.block != parent {
		unclosed = &unclosedLiterals{block: parent}
		pc.Set(unclosedLiteralsKey, unclosed)
	}
	if slices.Contains(unclosed.names, name) {
		return nil
	}

	// A table has already read "\|" as a pipe that does not end the cell.
	inCell := parent.Kind() == east.KindTableCell

	savedLine, savedSegment := block.Position()

	// The block is gathered a line at a time, and read only once a line
	// could close the element: most close on the line they open on.
	var (
		rest     []byte
		pieces   [][]byte
		consumed int
		closable bool
	)
	closer := []byte("</" + name)
	for pieces == nil {
		line, _ := block.PeekLine()
		if line == nil {
			break
		}
		rest = append(rest, line...)
		block.AdvanceLine()

		if bytes.Contains(line, closer) {
			closable = true
			pieces, consumed = literalContent(rest, name, inCell, false)
		}
	}
	if pieces == nil && closable {
		pieces, consumed = literalContent(rest, name, inCell, true)
	}

	block.SetPosition(savedLine, savedSegment)

	if pieces == nil {
		unclosed.names = append(unclosed.names, name)

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
		parent.AppendChild(rawHTML(piece))
	}

	return rawHTML(pieces[len(pieces)-1])
}

var unclosedLiteralsKey = parser.NewContextKey()

// unclosedLiterals are the literal elements that the rest of a block was
// read for a closing tag of and has none, so that a block of many opening
// tags is read once for each name and not once for each tag.
type unclosedLiterals struct {
	block ast.Node
	names []string
}

// rawHTML is a node of markup that is not a range of the source.
func rawHTML(markup []byte) ast.Node {
	return ast.NewRawHTML(text.NewMultiLineValueFromString(string(markup), text.IdentityDecoder))
}

// literalContent splits what follows the opening tag of the literal element
// called name into the markup of each node up to and including its closing
// tag, and reports how many bytes of rest that took. It returns nil when rest
// does not close the element.
//
// A Confluence tag, a CDATA section and a comment are each a piece as
// written: a parameter may hold an <ri:url/> or an <ac:link>. Everything else
// is text, escaped. The element ends at the first closing tag of its name:
// the storage format has no literal element inside another of the same name.
//
// A section rest does not close is text when rest is the whole of the block.
// When it is not, the section may close further on and hold what looks like
// the closing tag, so rest does not close the element yet.
func literalContent(rest []byte, name string, inCell, whole bool) (pieces [][]byte, consumed int) {
	textStart := 0

	for i := 0; i < len(rest); {
		if rest[i] != '<' {
			i++
			continue
		}

		tail := rest[i:]
		section := bytes.HasPrefix(tail, openCDATA) || bytes.HasPrefix(tail, openComment)

		var length int
		closes := false

		switch {
		case bytes.HasPrefix(tail, openCDATA):
			length = sectionLength(tail, closeCDATA)
		case bytes.HasPrefix(tail, openComment):
			length = sectionLength(tail, closeComment)
		default:
			if groups := closeTagRegexp.FindSubmatch(tail); groups != nil {
				length = len(groups[0])
				closes = string(groups[1])+":"+string(groups[2]) == name
			} else {
				length = len(openTagRegexp.Find(tail))
			}
		}

		if length == 0 {
			if section && !whole {
				return nil, 0
			}

			// A "<" that opens nothing: text.
			i++
			continue
		}

		if i > textStart {
			pieces = append(pieces, escapeLiteral(rest[textStart:i], inCell))
		}
		pieces = append(pieces, tail[:length])
		i += length
		textStart = i

		if closes {
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

// entityDecoder resolves character references the way goldmark does for the
// text of a paragraph.
var entityDecoder = text.NewDecoder()

// escapeLiteral is b as markup for the text it spells: a character reference
// is the character it names, as it is in any text, and nothing else has a
// meaning -- a backslash stays a backslash, which doubling it tells the
// decoder. In a table cell "\|" is the pipe the table read it as.
func escapeLiteral(b []byte, inCell bool) []byte {
	if inCell {
		b = bytes.ReplaceAll(b, []byte(`\|`), []byte(`|`))
	}
	b = bytes.ReplaceAll(b, []byte(`\`), []byte(`\\`))

	return util.EscapeHTML(entityDecoder.Decode(b))
}
