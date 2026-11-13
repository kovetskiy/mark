package parser_test

import (
	"strings"
	"testing"
	"time"

	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

// admonitions parses source and returns every admonition found, in document
// order, as "class:title:first child's text", indented by depth.
func admonitions(t *testing.T, source string) []string {
	t.Helper()

	md := parser.New(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	)

	src := []byte(source)
	doc := md.Parse(src)

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
		if first, ok := node.FirstChild().(ast.BlockNode); ok {
			for _, segment := range first.Source() {
				body += string(segment.Bytes(src))
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
	md := parser.New(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	)

	source := []byte("!!! danger \"D\" {.extra #id}\n    body\n")
	doc := md.Parse(source)
	admonition, ok := doc.FirstChild().(*cparser.Admonition)
	require.True(t, ok)

	var names []string
	for _, attribute := range admonition.Attributes() {
		names = append(names, attribute.Name+"="+attribute.Value.Value(source))
	}
	assert.ElementsMatch(t, []string{"class=admonition adm-danger extra", "id=id"}, names)
}

// outline parses source and returns its blocks in document order, one a line
// and indented by depth: an admonition as "!class", any other block as its
// kind and text.
func outline(t *testing.T, source string) []string {
	t.Helper()

	md := parser.New(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	)

	src := []byte(source)

	var lines []string
	var walk func(node ast.Node, depth string)
	walk = func(node ast.Node, depth string) {
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if admonition, ok := child.(*cparser.Admonition); ok {
				lines = append(lines, depth+"!"+string(admonition.AdmonitionClass))
				walk(child, depth+"  ")

				continue
			}

			block, ok := child.(ast.BlockNode)
			if !ok {
				continue
			}

			body := ""
			for _, segment := range block.Source() {
				body += string(segment.Bytes(src))
			}

			line := depth + child.Kind().String()
			if body = strings.TrimRight(body, "\n"); body != "" {
				line += ":" + body
			}
			lines = append(lines, line)
			walk(child, depth+"  ")
		}
	}
	walk(md.Parse(src), "")

	return lines
}

// TestAdmonitionBodyIsIndentedOrClosed: an admonition holds the lines indented
// under its opening line, as MkDocs reads it, or the lines up to its closing
// run. Lines that are neither follow it.
func TestAdmonitionBodyIsIndentedOrClosed(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "title only, then the rest of the document",
			source: "!!! note \"Remember\"\n\nNext paragraph.\n\n## Heading\n\nLast.\n",
			want:   []string{"!note", "Paragraph:Next paragraph.", "Heading:Heading", "Paragraph:Last."},
		},
		{
			name:   "unindented line straight after",
			source: "!!! tip\nunindented body\n\nlater paragraph\n",
			want:   []string{"!tip", "Paragraph:unindented body", "Paragraph:later paragraph"},
		},
		{
			name:   "indented body, then the rest of the document",
			source: "!!! note \"T\"\n    body\n\nNext paragraph.\n",
			want:   []string{"!note", "  Paragraph:body", "Paragraph:Next paragraph."},
		},
		{
			name:   "indented body after a blank line",
			source: "!!! note \"T\"\n\n    body\n\nNext paragraph.\n",
			want:   []string{"!note", "  Paragraph:body", "Paragraph:Next paragraph."},
		},
		{
			name:   "unindented body up to a closing run",
			source: "!!! note \"T\"\nbody\n\n## Heading\n!!!\n\nNext paragraph.\n",
			want:   []string{"!note", "  Paragraph:body", "  Heading:Heading", "Paragraph:Next paragraph."},
		},
		{
			name:   "unindented body after a blank line, up to a closing run",
			source: "!!! note \"T\"\n\nbody\n\n!!!\nNext paragraph.\n",
			want:   []string{"!note", "  Paragraph:body", "Paragraph:Next paragraph."},
		},
		{
			name:   "unindented and nested, each with its closing run",
			source: "!!! note \"outer\"\na\n!!! tip \"inner\"\nb\n!!!\nc\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:a", "  !tip", "    Paragraph:b", "  Paragraph:c", "Paragraph:after"},
		},
		{
			name:   "a closing run ends the inner one only",
			source: "!!! note \"outer\"\na\n!!! tip \"inner\"\nb\n!!!\nc\n",
			want:   []string{"!note", "Paragraph:a", "!tip", "  Paragraph:b", "Paragraph:c"},
		},
		{
			name:   "title only, then a sibling",
			source: "!!! note \"one\"\n\n!!! tip \"two\"\n    b\n\nafter\n",
			want:   []string{"!note", "!tip", "  Paragraph:b", "Paragraph:after"},
		},
		{
			name:   "title only inside an indented body",
			source: "!!! note \"outer\"\n    !!! tip \"inner\"\n\n    a\n\nafter\n",
			want:   []string{"!note", "  !tip", "  Paragraph:a", "Paragraph:after"},
		},
		{
			name:   "title only in a list item",
			source: "- item\n\n  !!! note \"T\"\n\n  in the item\n\nafter\n",
			want: []string{
				"List", "  ListItem", "    Paragraph:item", "    !note", "    Paragraph:in the item", "Paragraph:after",
			},
		},
		{
			name:   "title only in a list item, then the next item",
			source: "- one\n- !!! note \"T\"\n  in the item\n- three\n",
			want: []string{
				"List", "  ListItem", "    Paragraph:one", "  ListItem", "    !note", "    Paragraph:in the item",
				"  ListItem", "    Paragraph:three",
			},
		},
		{
			name:   "ended by the block that follows it",
			source: "  !!! note \"T\"\n  body\n> quoted\n",
			want:   []string{"!note", "Paragraph:body", "Blockquote", "  Paragraph:quoted"},
		},
		{
			name:   "a run in a fenced code block is code",
			source: "!!! note \"T\"\n\none\n\n```markdown\n!!! note \"X\"\nbody\n!!!\n```\n\nrest\n",
			want:   []string{"!note", "Paragraph:one", "CodeBlock", "Paragraph:rest"},
		},
		{
			name:   "a run in an HTML block is the block's",
			source: "!!! note \"T\"\n\ntext\n\n<pre>\n!!!\n</pre>\n\nend\n",
			want:   []string{"!note", "Paragraph:text", "HTMLBlock", "Paragraph:end"},
		},
		{
			name:   "a run in a fenced code block of a closed body is code",
			source: "!!! note \"T\"\n```\n!!!\n```\n<pre>\n!!!\n</pre>\n\n<!-- c -->\n!!!\nafter\n",
			want:   []string{"!note", "  CodeBlock", "  HTMLBlock", "  HTMLBlock", "Paragraph:after"},
		},
		{
			name:   "a run straight after a nested indented body closes that one",
			source: "!!! note \"outer\"\nintro\n!!! tip \"inner\"\n    b\n!!!\n\nafter\n",
			want:   []string{"!note", "Paragraph:intro", "!tip", "  Paragraph:b", "Paragraph:after"},
		},
		{
			name:   "each closed by its own run, the inner one indented",
			source: "!!! note \"outer\"\na\n!!! tip \"inner\"\n    b\n!!!\nc\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:a", "  !tip", "    Paragraph:b", "  Paragraph:c", "Paragraph:after"},
		},
		{
			name:   "title only, then one with an indented body and a run",
			source: "!!! note \"one\"\n\ntext\n\n!!! tip \"two\"\n    body\n!!!\n\nafter\n",
			want:   []string{"!note", "Paragraph:text", "!tip", "  Paragraph:body", "Paragraph:after"},
		},
		{
			// The paragraph of the inner body runs on over the line that
			// ended that body, and the run after it is the outer one's.
			name:   "a run after a nested indented body and a line level with it",
			source: "!!! note \"outer\"\n!!! tip \"inner\"\n    b\nlazy\n!!!\nafter\n",
			want:   []string{"!note", "  !tip", "    Paragraph:b\nlazy", "Paragraph:after"},
		},
		{
			name:   "a run too short for the one opened above it",
			source: "!!! note \"outer\"\na\n!!!! tip \"inner\"\nb\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:a", "  !tip", "  Paragraph:b", "Paragraph:after"},
		},
		{
			name:   "a run too short for the one opened on the line above",
			source: "!!! note \"outer\"\n!!!! tip \"inner\"\n!!!\nafter\n",
			want:   []string{"!note", "  !tip", "Paragraph:after"},
		},
		{
			name:   "a run straight after an HTML block that ends at a blank line",
			source: "!!! note \"T\"\n<details>\n<summary>s</summary>\n</details>\n!!!\n\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "Paragraph:after"},
		},
		{
			name:   "a run after a list item that opens with a code block",
			source: "!!! note \"T\"\n- ```\n  code\n  ```\n!!!\nafter\n",
			want:   []string{"!note", "  List", "    ListItem", "      CodeBlock", "Paragraph:after"},
		},
		{
			name:   "a code block cut short by a line indented less than the opening line",
			source: "  !!! note \"T\"\n  a\n  ```\ncode\n  ```\n  !!!\nafter\n",
			want:   []string{"!note", "Paragraph:a", "CodeBlock", "Paragraph:!!!\nafter"},
		},
		{
			name:   "closed by a run behind a partly taken tab",
			source: "- item\n\n\t!!! note \"x\"\n\tb\n\t!!!\n\tafter\n",
			want: []string{
				"List", "  ListItem", "    Paragraph:item", "    !note", "      Paragraph:b", "    Paragraph:after",
			},
		},
		{
			name:   "closed by a run after a quote marker without its blank",
			source: "> !!! note \"T\"\n> a\n>!!!\n> c\n",
			want:   []string{"Blockquote", "  !note", "    Paragraph:a", "  Paragraph:c"},
		},
		{
			name:   "a run after a title-only one nested in an indented body",
			source: "!!! note \"outer\"\n    !!! tip \"inner\"\n\n    a\n!!!\n\nafter\n",
			want:   []string{"!note", "  !tip", "  Paragraph:a", "Paragraph:after"},
		},
		{
			name:   "a run after two indented bodies, one in the other",
			source: "!!! note \"outer\"\n    !!! tip \"inner\"\n        b\n!!!\nafter\n",
			want:   []string{"!note", "  !tip", "    Paragraph:b", "Paragraph:after"},
		},
		{
			name:   "title only, then one closed by a run",
			source: "!!! note \"one\"\n!!! tip \"two\"\nb\n!!!\nafter\n",
			want:   []string{"!note", "!tip", "  Paragraph:b", "Paragraph:after"},
		},
		{
			name:   "a run in the next list item is not this one's",
			source: "- !!! note \"T\"\n  a\n- !!! tip \"U\"\n  b\n  !!!\n",
			want: []string{
				"List", "  ListItem", "    !note", "    Paragraph:a", "  ListItem", "    !tip", "      Paragraph:b",
			},
		},
		{
			name:   "closed by a run in a blockquote",
			source: "> !!! note \"T\"\n> a\n>\n> b\n> !!!\n> c\n",
			want:   []string{"Blockquote", "  !note", "    Paragraph:a", "    Paragraph:b", "  Paragraph:c"},
		},
		{
			name:   "title only in a blockquote",
			source: "> !!! note \"T\"\n>\n> quoted\n\nafter\n",
			want:   []string{"Blockquote", "  !note", "  Paragraph:quoted", "Paragraph:after"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, outline(t, tt.source))
		})
	}
}

// TestAdmonitionsWithoutABodyAreNotNested: an admonition that ends at its
// opening line is closed there, so a page of them is read in one pass rather
// than each inside the one before, and not to its last closing run once for
// each either.
func TestAdmonitionsWithoutABodyAreNotNested(t *testing.T) {
	const count = 20000

	for name, tail := range map[string]string{
		"no run":                 "",
		"a run at the end":       "!!!\n",
		"a run in code":          "```\n!!!\n```\n",
		"a run in a list at end": "- !!!\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := strings.Repeat("!!! note \"T\"\n\ntext\n\n", count) + tail

			start := time.Now()
			blocks := outline(t, source)

			assert.Equal(t, []string{"!note", "Paragraph:text", "!note", "Paragraph:text"}, blocks[:4])
			assert.Equal(t, "!note", blocks[2*count-2])

			// Read once for each, a page this long takes minutes.
			assert.Less(t, time.Since(start), 20*time.Second)
		})
	}
}
