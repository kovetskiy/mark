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
// A Confluence tag, a CDATA section and a comment are each a node as
// written: a parameter may hold an <ri:url/> or an <ac:link>, and a section
// what looks like the closing tag. Everything else is text, escaped. The
// element ends at the first closing tag of its name: the storage format has
// no literal element inside another of the same name.
//
// Without a closing tag in this block it returns nil and appends nothing, and
// the tag is an opening tag like any other.
func (s *confluenceTagParser) parseLiteralContent(
	parent, open ast.Node, name string, block text.Reader, pc parser.Context,
) ast.Node {
	unclosed, _ := pc.Get(unclosedLiteralsKey).(*unclosedLiterals)
	if unclosed == nil || unclosed.block != parent {
		unclosed = &unclosedLiterals{block: parent, cdataFrom: -1, commentFrom: -1}
		pc.Set(unclosedLiteralsKey, unclosed)
	}
	if slices.Contains(unclosed.names, name) {
		return nil
	}

	// A table has already read "\|" as a pipe that does not end the cell.
	inCell := parent.Kind() == east.KindTableCell

	savedLine, savedSegment := block.Position()

	var (
		nodes   []ast.Node
		literal []byte
	)
	for {
		line, _ := block.PeekLine()
		if line == nil {
			break
		}

		start := bytes.IndexByte(line, '<')
		if start < 0 {
			literal = append(literal, line...)
			block.AdvanceLine()

			continue
		}
		literal = append(literal, line[:start]...)
		block.Advance(start)
		line = line[start:]

		var node ast.Node
		closes := false

		switch {
		case bytes.HasPrefix(line, openCDATA):
			node = s.parseSection(block, closeCDATA, &unclosed.cdataFrom, pc)
		case bytes.HasPrefix(line, openComment):
			node = s.parseSection(block, closeComment, &unclosed.commentFrom, pc)
		case bytes.HasPrefix(line, []byte("</")):
			node = s.parseMultiLineRegexp(closeTagRegexp, block, pc)
			closes = node != nil && closesElement(node.(*ast.RawHTML).Value.Bytes(block.Source()), name)
		default:
			node = s.parseMultiLineRegexp(openTagRegexp, block, pc)
		}

		if node == nil {
			// A "<" that opens nothing: text.
			literal = append(literal, '<')
			block.Advance(1)

			continue
		}

		if len(literal) > 0 {
			nodes = append(nodes, rawHTML(escapeLiteral(literal, inCell)))
			literal = literal[:0]
		}
		if closes {
			parent.AppendChild(open)
			for _, child := range nodes {
				parent.AppendChild(child)
			}

			return node
		}
		nodes = append(nodes, node)
	}

	block.SetPosition(savedLine, savedSegment)
	unclosed.names = append(unclosed.names, name)

	return nil
}

var unclosedLiteralsKey = parser.NewContextKey()

// unclosedLiterals is what the rest of a block was read for and does not
// have, so that the block is read for it once and not once from each place
// it is opened: the literal elements with no closing tag, and the offsets
// from which a CDATA section or a comment has no end (-1 while not known).
type unclosedLiterals struct {
	block                  ast.Node
	names                  []string
	cdataFrom, commentFrom int
}

// parseSection reads a CDATA section or a comment up to closer, unless the
// block is known to have no closer from where it opens.
func (s *confluenceTagParser) parseSection(
	block text.Reader, closer []byte, unclosedFrom *int, pc parser.Context,
) ast.Node {
	_, segment := block.Position()
	if *unclosedFrom >= 0 && segment.Start >= *unclosedFrom {
		return nil
	}

	node := s.parseUntil(block, closer, pc)
	if node == nil {
		*unclosedFrom = segment.Start
	}

	return node
}

// rawHTML is a node of markup that is not a range of the source.
func rawHTML(markup []byte) ast.Node {
	return ast.NewRawHTML(text.NewMultiLineValueFromString(string(markup), text.IdentityDecoder))
}

// closesElement reports whether tag is the closing tag of the element called
// name, and not that of one whose name it begins.
func closesElement(tag []byte, name string) bool {
	rest, ok := bytes.CutPrefix(tag, []byte("</"+name))

	return ok && bytes.IndexAny(rest[:1], " \t\r\n>") == 0
}

var openComment = []byte("<!--")
var closeComment = []byte("-->")

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
