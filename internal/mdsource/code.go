package mdsource

import (
	"sync"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Region is a half-open byte range within a document.
type Region struct {
	Start int
	Stop  int
}

// Contains reports whether offset falls inside the region.
func (r Region) Contains(offset int) bool {
	return offset >= r.Start && offset < r.Stop
}

// InCode reports whether offset falls inside any of the regions.
func InCode(regions []Region, offset int) bool {
	for _, region := range regions {
		if region.Contains(offset) {
			return true
		}
	}

	return false
}

// CodeRegions reports where the code is in a document.
//
// Fenced blocks, indented blocks and code spans, which between them are every
// place a reader is being shown text rather than told something. Anything that
// rewrites a document -- a macro, an include directive, an ignore marker -- has
// to leave these alone, or a page documenting the feature has its own example
// eaten by it.
//
// Asking the parser rather than looking for backticks is deliberate: tildes,
// indented blocks, closing fences longer than their opener and nesting are all
// easy to get subtly wrong, and goldmark already knows.
//
// The parser reads footnotes, definition lists and tables, as the one a
// document is compiled with always does. A footnote and a definition hold their
// later paragraphs indented by four spaces, which without them is an indented
// code block; and a line starting with a colon defines nothing after a table,
// which without the table parser is a paragraph it would make a term of.
func CodeRegions(data []byte) []Region {
	pc := parser.NewContext()
	codeParser().Parse(data, parser.WithContext(pc))

	regions, _ := pc.Get(codeRegionsKey).([]Region)

	return regions
}

// codeParser is built once: a goldmark parser keeps no state from one Parse to
// the next, and neither do the parsers and the transformer it holds.
var codeParser = sync.OnceValue(func() parser.Parser {
	return parser.New(
		parser.WithExtensions(
			extension.NewFootnoteParser(),
			extension.NewDefinitionListParser(),
			extension.NewTableParser(),
		),
		// Ahead of the table parser's own transformer at 0, which gives a code
		// span holding an escaped pipe a value with no place in the source.
		parser.WithASTTransformers(util.Prioritized[parser.ASTTransformer](codeCollector{}, -1)),
	)
})

// codeRegionsKey is where codeCollector leaves the regions of one parse.
var codeRegionsKey = parser.NewContextKey()

// codeCollector gathers the code regions of the document it is run on.
type codeCollector struct{}

func (codeCollector) Transform(doc *ast.Document, _ text.Reader, pc parser.Context) {
	var regions []Region

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.CodeBlock:
			// Fenced and indented blocks alike: goldmark has the one node
			// for both.
			lines := n.Value.Segments()
			if len(lines) == 0 {
				return ast.WalkContinue, nil
			}
			regions = append(regions, Region{
				Start: lines[0].Start,
				Stop:  lines[len(lines)-1].Stop,
			})

		case *ast.CodeSpan:
			// A span's content is its value, one index per source line.
			indices := n.Value.Indices()
			if len(indices) == 0 || n.Value.IsEmpty() {
				return ast.WalkContinue, nil
			}
			regions = append(regions, Region{
				Start: indices[0].Start,
				Stop:  indices[len(indices)-1].Stop,
			})
		}

		return ast.WalkContinue, nil
	})

	pc.Set(codeRegionsKey, regions)
}
