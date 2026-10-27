package htmlblock

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
)

// TestLines pins the lines of a block, its closing line left out, for each
// kind of block that has a closing line and for the kinds that end on a blank
// line instead.
func TestLines(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"comment closed on its first line", "<!-- a -->\n", []string{"<!-- a -->\n"}},
		{"comment closed on a later line", "<!-- a\nb\nc -->\n", []string{"<!-- a\n", "b\n"}},
		{"comment closed on its second line", "<!--\nWarn -->\n", []string{"<!--\n"}},
		{"comment never closed", "<!-- a\nb\n", []string{"<!-- a\n", "b\n"}},
		{"script", "<script>\nx\n</SCRIPT>\n", []string{"<script>\n", "x\n"}},
		{"processing instruction", "<?php\nx\n?>\n", []string{"<?php\n", "x\n"}},
		{"declaration", "<!DOCTYPE\nhtml>\n", []string{"<!DOCTYPE\n"}},
		{"CDATA", "<![CDATA[\nx\n]]>\n", []string{"<![CDATA[\n", "x\n"}},
		{"block ended by a blank line", "<div>\nx -->\n\n", []string{"<div>\n", "x -->\n"}},
		{"comment in a blockquote", "> <!--\n> Warn -->\n", []string{"<!--\n"}},
		{"comment cut off by its blockquote", "> <!-- a\n> b\n\nc -->\n", []string{"<!-- a\n", "b\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.input)
			doc := parser.New().Parse(source)

			var block *ast.HTMLBlock
			_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
				if b, ok := node.(*ast.HTMLBlock); ok && entering && block == nil {
					block = b
				}

				return ast.WalkContinue, nil
			})
			require.NotNil(t, block)

			var got []string
			for _, line := range Lines(block, source) {
				got = append(got, line.Str(source))
			}

			assert.Equal(t, tc.want, got, strings.TrimSpace(tc.input))
		})
	}
}
