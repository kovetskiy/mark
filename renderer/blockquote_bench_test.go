package renderer_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/internal/goldmarktest"
	crenderer "github.com/kovetskiy/mark/v17/renderer"
	"github.com/yuin/goldmark/v2/ast"
)

// BenchmarkParseBlockQuoteType classifies one quote, as the renderer does for
// every blockquote it meets.
func BenchmarkParseBlockQuoteType(b *testing.B) {
	source := []byte("> Warn: this deletes data.\n")
	doc := goldmarktest.New().Parse(source)

	var quote ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == ast.KindBlockquote {
			quote = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if quote == nil {
		b.Fatal("no blockquote parsed")
	}

	b.ReportAllocs()
	for b.Loop() {
		if crenderer.ParseBlockQuoteType(quote, source) != crenderer.AdmonitionNote {
			b.Fatal("misclassified")
		}
	}
}
