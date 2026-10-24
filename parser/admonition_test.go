package parser_test

import (
	"strings"
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
		{
			name:   "collapsed",
			source: "??? note \"Title\"\n    body\n",
			want:   []string{`note:"Title":body`},
		},
		{
			name:   "collapsed, open by default",
			source: "???+ tip \"Title\"\n    body\n",
			want:   []string{`tip:"Title":body`},
		},
		{
			name:   "collapsed without a title",
			source: "??? warning\n    body\n",
			want:   []string{`warning::body`},
		},
		{
			name:   "collapsed closed by its own run",
			source: "??? note \"c\"\n    a\n???\n\nafter\n",
			want:   []string{`note:"c":a`},
		},
		{
			// A closing run is made of the character that opened the block.
			name:   "a run of the other character does not close",
			source: "??? note \"c\"\n    a\n!!!\n\n    b\n",
			want:   []string{"note:\"c\":a\n!!!"},
		},
		{
			name:   "bang run does not close on question marks",
			source: "!!! note \"c\"\n    a\n???\n",
			want:   []string{"note:\"c\":a\n???"},
		},
		{
			name:   "bare question marks are text",
			source: "???\n\n???+\n\n?? note\n",
			want:   nil,
		},
		{
			name:   "question marks opening prose are text",
			source: "??? what is this\n\npara\n??? what is\ncont\n\n???? really sure\n",
			want:   nil,
		},
		{
			name:   "one word of prose before an unindented line is text",
			source: "??? Really?\nThis is prose.\n\n??? what\nnext\n\n??? what\n\nnext para\n\n??? what\n",
			want:   nil,
		},
		{
			name:   "a bare class opens when an indented body follows, blank lines or not",
			source: "??? FAQ\n    answer\n\n??? tip\n\n\n    spaced\n",
			want:   []string{"FAQ::answer", "tip::spaced"},
		},
		{
			name:   "a bare class opens an empty block that closes straight away",
			source: "??? note\n???\n",
			want:   []string{"note::"},
		},
		{
			name:   "a class that is not a word is text",
			source: "??? \"T\"\n    x\n\n??? ?\n    x\n\n??? +note\n    x\n\n??? note\"T\"\n    x\n",
			want:   nil,
		},
		{
			name:   "a title is one closed quoted string",
			source: "??? what \"is\" this\n    x\n\n??? note \"T\n    x\n\n??? note \"T\" tail\n    x\n\n??? note \"T\" {.a}\n    x\n",
			want:   []string{`note:"T":x`},
		},
		{
			name:   "a tab ends no class, as with !!!",
			source: "??? note\t\"T\"\n    x\n",
			want:   nil,
		},
		{
			name:   "a bare class in a list is not judged by the next line",
			source: "- ??? what\n  next\n",
			want:   []string{"what::next"},
		},
		{
			name:   "collapsed with attributes and no title",
			source: "??? note {.x}\n    a\n",
			want:   []string{"note::a"},
		},
		{
			name:   "nested collapsed in a panel",
			source: "!!! note \"outer\"\n    a\n\n    ??? tip \"inner\"\n        b\n\n    c\n",
			want:   []string{`note:"outer":a`, `  tip:"inner":b`},
		},
		{
			name:   "collapsed sibling straight after",
			source: "??? info \"one\"\n    a\n???+ warning \"two\"\n    b\n",
			want:   []string{`info:"one":a`, `warning:"two":b`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, admonitions(t, tt.source))
		})
	}
}

func TestAdmonitionCollapsedFlag(t *testing.T) {
	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	))

	for source, want := range map[string]bool{
		"??? note\n    b\n":  true,
		"???+ note\n    b\n": false,
		"!!! note\n    b\n":  false,
	} {
		doc := md.Parser().Parse(text.NewReader([]byte(source)))
		admonition, ok := doc.FirstChild().(*cparser.Admonition)
		require.True(t, ok, source)
		assert.Equal(t, want, admonition.Collapsed, source)
		assert.Equal(t, "note", string(admonition.AdmonitionClass), source)
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
