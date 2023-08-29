package parser

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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

func (s *confluenceTagParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) > 1 && util.IsAlphaNumeric(line[1]) {
		return s.parseMultiLineRegexp(openTagRegexp, block, pc)
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

var attributePattern = `(?:[\r\n \t]+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:[\r\n \t]*=[\r\n \t]*(?:[^\"'=<>` + "`" + `\x00-\x20]+|'[^']*'|"[^"]*"))?)`

// Only match <ac:*/> and <ri:*/> tags
var openTagRegexp = regexp.MustCompile("^<(ac|ri):" + tagnamePattern + attributePattern + `*[ \t]*/?>`)
var closeTagRegexp = regexp.MustCompile("^</ac:" + tagnamePattern + `\s*>`)

var openCDATA = []byte("<![CDATA[")
var closeCDATA = []byte("]]>")
var closeDecl = []byte(">")

func (s *confluenceTagParser) parseUntil(block text.Reader, closer []byte, _ parser.Context) ast.Node {
	savedLine, savedSegment := block.Position()
	node := ast.NewRawHTML()
	for {
		line, segment := block.PeekLine()
		if line == nil {
			break
		}
		index := bytes.Index(line, closer)
		if index > -1 {
			node.Segments.Append(segment.WithStop(segment.Start + index + len(closer)))
			block.Advance(index + len(closer))
			return node
		}
		node.Segments.Append(segment)
		block.AdvanceLine()
	}
	block.SetPosition(savedLine, savedSegment)
	return nil
}

func (s *confluenceTagParser) parseMultiLineRegexp(reg *regexp.Regexp, block text.Reader, _ parser.Context) ast.Node {
	sline, ssegment := block.Position()
	if block.Match(reg) {
		node := ast.NewRawHTML()
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

			node.Segments.Append(text.NewSegment(start, end))
			if l == eline {
				block.Advance(end - start)
				break
			} else {
				block.AdvanceLine()
			}
		}
		return node
	}
	return nil
}
