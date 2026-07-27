package transformer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// AutoLinkTransformer walks the AST and adds data-card-appearance="inline" attributes to bare autolink URLs.
type AutoLinkTransformer struct{}

// NewAutoLinkTransformer creates a new AutoLinkTransformer instance.
func NewAutoLinkTransformer() *AutoLinkTransformer {
	return &AutoLinkTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *AutoLinkTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if node.Kind() == ast.KindAutoLink {
			node.SetAttribute([]byte("data-card-appearance"), []byte("inline"))
		}

		return ast.WalkContinue, nil
	})
}
