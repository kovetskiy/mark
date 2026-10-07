package parser_test

import (
	"strings"
	"testing"

	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// admonitions parses source and returns every admonition found, in document
// order, as "class:title:first child's text", indented by depth.
func admonitions(t *testing.T, source string) []string {
	t.Helper()

	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	))

	src := []byte(source)
	doc := md.Parser().Parse(text.NewReader(src))

	var found []string
	require.NoError(t, ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		admonition, ok := node.(*cparser.Admonition)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}

		depth := ""
		for p := node.Parent(); p != nil; p = p.Parent() {
			if _, ok := p.(*cparser.Admonition); ok {
				depth += "  "
			}
		}

		body := ""
		if first := node.FirstChild(); first != nil {
			lines := first.Lines()
			for i := 0; i < lines.Len(); i++ {
				segment := lines.At(i)
				body += string(segment.Value(src))
			}
		}

		found = append(found, depth+string(admonition.AdmonitionClass)+":"+string(admonition.Title)+":"+strings.TrimRight(body, "\n"))
		return ast.WalkContinue, nil
	}))

	return found
}

func TestAdmonitionParser(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "indented body",
			source: "!!! note \"Title\"\n    body\n",
			want:   []string{`note:"Title":body`},
		},
		{
			name:   "no class but a run",
			source: "!!!\n\n!! note\n",
			want:   nil,
		},
		{
			// The library this parser came from lost track of an admonition
			// opened and closed with nothing in it, and left the next one's
			// closing run in its body as text.
			name:   "closing run at least as long as the opening one",
			source: "!!! note \"empty\"\n!!!\n\n!!!! note \"four\"\n    a\n!!!!\n\nafter\n",
			want:   []string{`note:"empty":`, `note:"four":a`},
		},
		{
			// goldmark opens the second admonition before it closes the
			// first, so closing the first must not forget the second.
			name:   "sibling straight after",
			source: "!!! info \"one\"\n    a\n!!! warning \"two\"\n    b\n",
			want:   []string{`info:"one":a`, `warning:"two":b`},
		},
		{
			name:   "nested",
			source: "!!! note \"outer\"\n    a\n\n    !!! tip \"inner\"\n        b\n\n    c\n",
			want:   []string{`note:"outer":a`, `  tip:"inner":b`},
		},
		{
			// A lazy line, indented less than the body. The library this
			// parser came from stepped over the body's indentation in bytes
			// whatever was there, and published "cond line".
			name:   "lazy line keeps its text",
			source: "!!! note \"N\"\n    first line\n  second line\n",
			want:   []string{`note:"N":first line` + "\n" + `second line`},
		},
		{
			// The opening line was measured as though it always ended in a
			// newline, so a one-letter class was taken for a bare run.
			name:   "one-letter class",
			source: "!!! a\n    body\n",
			want:   []string{`a::body`},
		},
		{
			// The same off-by-one at the end of a document cut the last
			// character off the line: the title lost its closing quote.
			name:   "opening line without a newline",
			source: "!!! note \"T\"",
			want:   []string{`note:"T":`},
		},
		{
			name:   "class only, without a newline",
			source: "!!! note",
			want:   []string{`note::`},
		},
		{
			// A closing run behind a tab the list item only partly took
			// carries padding. Stepping over it twice ran past the end of
			// the line, and the next one was read into the admonition.
			name:   "closing run behind a partly taken tab",
			source: "- item\n\n\t!!! note \"x\"\n\t    b\n\t!!!\n\tafter\n",
			want:   []string{`note:"x":b`},
		},
		{
			name:   "tab-indented body",
			source: "!!! note \"tab\"\n\tbody\n",
			want:   []string{`note:"tab":body`},
		},
		{
			name:   "in a list item",
			source: "- item\n\n    !!! tip \"in list\"\n        inside\n\n- next\n",
			want:   []string{`tip:"in list":inside`},
		},
		{
			name:   "in a blockquote",
			source: "> !!! note \"in quote\"\n>     q\n",
			want:   []string{`note:"in quote":q`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, admonitions(t, tt.source))
		})
	}
}

// TestAdmonitionParserIsDeterministic: the parser keeps no attribute of its
// own on the node, so nothing random can reach the page.
func TestAdmonitionParserIsDeterministic(t *testing.T) {
	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	))

	doc := md.Parser().Parse(text.NewReader([]byte("!!! danger \"D\" {.extra #id}\n    body\n")))
	admonition, ok := doc.FirstChild().(*cparser.Admonition)
	require.True(t, ok)

	var names []string
	for _, attribute := range admonition.Attributes() {
		names = append(names, string(attribute.Name)+"="+string(attribute.Value.([]byte)))
	}
	assert.ElementsMatch(t, []string{"class=admonition adm-danger extra", "id=id"}, names)
}
