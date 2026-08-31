package parser_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// formulas parses source and returns every formula found, in order, as
// "display?equation".
func formulas(t *testing.T, source string) []string {
	t.Helper()

	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithInlineParsers(util.Prioritized(cparser.NewMathParser(), 99)),
	))

	doc := md.Parser().Parse(text.NewReader([]byte(source)))

	var found []string
	require.NoError(t, ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if math, ok := node.(*cparser.Math); ok {
			prefix := "inline:"
			if math.Display {
				prefix = "display:"
			}
			found = append(found, prefix+string(math.Equation))
		}
		return ast.WalkContinue, nil
	}))

	return found
}

// TestMathDelimiters covers the four ways to write a formula. All four are
// documented, and before this parser only the two dollar forms worked -- the
// TeX forms were published as their own source.
func TestMathDelimiters(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{"dollar is inline", `An $E = mc^2$ formula.`, []string{"inline:E = mc^2"}},
		{"double dollar is display", `$$E = mc^2$$`, []string{"display:E = mc^2"}},
		{"paren is inline", `An \(E = mc^2\) formula.`, []string{"inline:E = mc^2"}},
		{"bracket is display", `\[E = mc^2\]`, []string{"display:E = mc^2"}},
		{"several in one line", `$a$ and $b$`, []string{"inline:a", "inline:b"}},
		{"display may span lines", "$$\na + b\n$$", []string{"display:\na + b\n"}},
		{"markup inside is not markup", `$a_1 * b_2$`, []string{"inline:a_1 * b_2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formulas(t, tt.source))
		})
	}
}

// TestMathLeavesProseAlone covers the reason the dollar form needs a rule at
// all: "$" is a currency symbol far more often than it is a formula delimiter,
// and a parser that takes every pair of them turns a sentence about money into
// mathematics.
func TestMathLeavesProseAlone(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"two prices", `It costs $5 and $7 today.`},
		{"space after the opener", `Between $ 5 and 7$ dollars.`},
		{"space before the closer", `Between $5 and 7 $ dollars.`},
		{"one dollar", `It costs $5.`},
		{"empty pair", `Nothing here: $$`},
		{"an escaped dollar", `Costs \$5 and \$7.`},
		{"a lone backslash", `A \ backslash.`},
		{"unclosed inline", `An $unclosed formula.`},
		{"inline does not run past the line", "An $unclosed\nand a $ on the next line."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, formulas(t, tt.source))
		})
	}
}

// TestMathInCodeIsNotMath covers the other half of leaving prose alone: a code
// span or a code block is quoting the syntax, not using it.
func TestMathInCodeIsNotMath(t *testing.T) {
	assert.Empty(t, formulas(t, "A code span `$E = mc^2$` here."))
	assert.Empty(t, formulas(t, "```\n$E = mc^2$\n```\n"))
}
