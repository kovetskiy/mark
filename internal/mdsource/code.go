package mdsource

import (
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
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
func CodeRegions(data []byte) []Region {
	var regions []Region

	doc := parser.New().Parse(data)

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

	return regions
}
