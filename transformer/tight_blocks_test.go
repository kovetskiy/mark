package transformer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

// TestTightBlockTransformer pins which paragraphs become TextBlocks: those of
// a tight list's items and of a tight definition description, however deeply
// nested, the ones beside each other included, and nothing else.
func TestTightBlockTransformer(t *testing.T) {
	p := parser.New(
		parser.WithExtensions(extension.DefinitionListParser),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](NewTightBlockTransformer(), 0),
		),
	)

	for _, tc := range []struct {
		name, input, want string
	}{
		{"no lists", "a\n\n> b\n\n# c\n", "P(a) P(b)"},
		{"tight list", "- a\n- b\n", "T(a) T(b)"},
		{"loose list", "- a\n\n- b\n", "P(a) P(b)"},
		{
			"nested tight list in a loose one",
			"- a\n\n- b\n  - c\n  - d\n",
			"P(a) P(b) T(c) T(d)",
		},
		{"tight list in a blockquote", "> - a\n> - b\n", "T(a) T(b)"},
		{"tight list item with a blockquote", "- a\n  > b\n- c\n", "T(a) P(b) T(c)"},
		{"tight definition", "Term\n: a\n: b\n", "T(a) T(b)"},
		{"loose definition", "Term\n\n: a\n", "P(a)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.input)
			doc := p.Parse(source)

			var got []string
			_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
				if !entering {
					return ast.WalkContinue, nil
				}

				switch node.(type) {
				case *ast.Paragraph:
					got = append(got, "P("+NodeText(node, source)+")")
				case *TextBlock:
					got = append(got, "T("+NodeText(node, source)+")")
				}

				return ast.WalkContinue, nil
			})

			assert.Equal(t, tc.want, strings.Join(got, " "))
		})
	}
}
