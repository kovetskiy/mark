package parser_test

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/parsenote"
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
	walk(admonitionBlocks.Parse(src), "")

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
			name:   "a run in a fenced code block indented a little is code",
			source: "!!! note \"T\"\nbody\n ```\n!!!\n ```\nafter\n\n## Heading\n",
			want:   []string{"!note", "Paragraph:body", "CodeBlock", "Paragraph:after", "Heading:Heading"},
		},
		{
			name:   "a run in an HTML block indented a little is the block's",
			source: "!!! note \"T\"\nbody\n  <pre>\n!!!\n  </pre>\nafter\n\n## Heading\n",
			want:   []string{"!note", "Paragraph:body", "HTMLBlock", "Paragraph:after", "Heading:Heading"},
		},
		{
			name:   "a run after a fenced code block indented a little, which holds one",
			source: "!!! note \"T\"\nbody\n ```\n!!!\n ```\nmore\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:body", "  CodeBlock", "  Paragraph:more", "Paragraph:after"},
		},
		{
			name:   "a run in a fenced code block after a list item, indented less than the item",
			source: "!!! note \"T\"\n- item\n\n ~~~\n!!!\n ~~~\nmore\n!!!\nafter\n",
			want: []string{
				"!note", "  List", "    ListItem", "      Paragraph:item", "  CodeBlock", "  Paragraph:more", "Paragraph:after",
			},
		},
		{
			name:   "a run in a fenced code block after a thematic break",
			source: "!!! note \"T\"\n* * *\n  ```\n!!!\n  ```\nafter\n\n## Heading\n",
			want:   []string{"!note", "ThematicBreak", "CodeBlock", "Paragraph:after", "Heading:Heading"},
		},
		{
			// The comment is text of the block that "<div>" opens, which
			// goes on to a blank line, and a run ends that block as it does
			// a paragraph.
			name:   "a run after a comment opened in a div",
			source: "!!! note \"T\"\n<div>\n<!-- c\n!!!\n-->\n</div>\n\nx\n!!!\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "Paragraph:-->", "HTMLBlock", "Paragraph:x\n!!!\nafter"},
		},
		{
			name:   "an opening line in an HTML block that ends at a blank line opens nothing",
			source: "!!! note \"T\"\n<div>\n!!! tip \"in the block\"\n</div>\n\ntext\n!!!\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "  Paragraph:text", "Paragraph:after"},
		},
		{
			name:   "an opening line in a block opened by a tag alone on its line opens nothing",
			source: "!!! note \"T\"\n\n<span>\n!!! tip \"in the block\"\n</span>\n\n!!!\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "Paragraph:after"},
		},
		{
			name:   "a tag alone on a line of a paragraph opens no block",
			source: "!!! note \"T\"\ntext\n<span>\n!!! tip \"U\"\nb\n!!!\nc\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:text\n<span>", "  !tip", "    Paragraph:b", "  Paragraph:c", "Paragraph:after"},
		},
		{
			name:   "a code fence in an HTML block that ends at a blank line is its text",
			source: "!!! note \"T\"\n<div>\n```\n</div>\n\ntext\n!!!\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "  Paragraph:text", "Paragraph:after"},
		},
		{
			name:   "an unclosed comment in an HTML block that ends at a blank line is its text",
			source: "!!! note \"T\"\n<div>\n<!--\n</div>\n\ntext\n!!!\nafter\n",
			want:   []string{"!note", "  HTMLBlock", "  Paragraph:text", "Paragraph:after"},
		},
		{
			name:   "a code block indented like the list item a nested, closed body opened with",
			source: "!!! note \"outer\"\n!!! tip \"inner\"\n1. item\n!!!\n   ```\n   !!!\n```\nmore\n!!!\nafter\n",
			want: []string{
				"!note", "  !tip", "    List", "      ListItem", "        Paragraph:item", "  CodeBlock", "  Paragraph:more",
				"Paragraph:after",
			},
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
			// The run is as long as the outer one's, which is longer than the
			// inner one's.
			name:   "a longer run straight after a nested indented body closes the outer one",
			source: "!!!! quote \"T\"\nw2\n\n!!! note \"N\"\n    w5\n\n!!!!\nafter\n",
			want:   []string{"!quote", "  Paragraph:w2", "  !note", "    Paragraph:w5", "Paragraph:after"},
		},
		{
			name:   "a longer run straight after a nested indented body, at the end of the document",
			source: "!!!! quote \"T\"\nw2\n\n!!! note \"N\"\n    w5\n\n!!!!\n",
			want:   []string{"!quote", "  Paragraph:w2", "  !note", "    Paragraph:w5"},
		},
		{
			// The first run closes the outer one, and the second nothing.
			name:   "a longer run straight after a nested indented body, and one after it",
			source: "!!!! quote \"T\"\nw2\n!!! note \"N\"\n    w5\n!!!!\nw7\n!!!!\nafter\n",
			want:   []string{"!quote", "  Paragraph:w2", "  !note", "    Paragraph:w5", "Paragraph:w7\n!!!!\nafter"},
		},
		{
			name:   "each closed by its own run, the outer one opened by a longer run",
			source: "!!!! quote \"T\"\nw2\n!!! note \"N\"\n    w5\n!!!\n!!!!\nafter\n",
			want:   []string{"!quote", "  Paragraph:w2", "  !note", "    Paragraph:w5", "Paragraph:after"},
		},
		{
			name:   "a longer run, title only, then one with an indented body and a run of its own",
			source: "!!!! quote \"T\"\n\ntext\n\n!!! note \"N\"\n    w5\n!!!\n\nafter\n",
			want:   []string{"!quote", "Paragraph:text", "!note", "  Paragraph:w5", "Paragraph:after"},
		},
		{
			// The paragraph of the inner body runs on over the line that
			// ended that body, and the run after it is the outer one's.
			name:   "a run after a nested indented body and a line level with it",
			source: "!!! note \"outer\"\n!!! tip \"inner\"\n    b\nlazy\n!!!\nafter\n",
			want:   []string{"!note", "  !tip", "    Paragraph:b\nlazy", "Paragraph:after"},
		},
		{
			name:   "a run after a line of the body's paragraph that lacks the list item's indentation",
			source: "- !!! note\n  text\nlazy\n  !!!\n  after\n",
			want:   []string{"List", "  ListItem", "    !note", "      Paragraph:text\nlazy", "    Paragraph:after"},
		},
		{
			name:   "a run after two such lines, in an ordered list item",
			source: "1. !!! note\n   text\nlazy one\nlazy two\n   !!!\n\nafter\n",
			want:   []string{"List", "  ListItem", "    !note", "      Paragraph:text\nlazy one\nlazy two", "Paragraph:after"},
		},
		{
			name:   "a run after a line of the body's paragraph that lacks the quote marker",
			source: "> !!! note\n> text\nlazy\n> !!!\n",
			want:   []string{"Blockquote", "  !note", "    Paragraph:text\nlazy"},
		},
		{
			name:   "a run after a line that lacks the indentation of a list item in a blockquote",
			source: "> - !!! note\n>   text\n> lazy\n>   !!!\n",
			want:   []string{"Blockquote", "  List", "    ListItem", "      !note", "        Paragraph:text\nlazy"},
		},
		{
			name:   "a run after a line that lacks both the quote marker and the list item's indentation",
			source: "> - !!! note\n>   text\nlazy\n>   !!!\n",
			want:   []string{"Blockquote", "  List", "    ListItem", "      !note", "        Paragraph:text\nlazy"},
		},
		{
			name:   "a run after a line of the body's paragraph indented less than the opening line",
			source: "  !!! note\n  text\nlazy\n  !!!\nafter\n",
			want:   []string{"!note", "  Paragraph:text\nlazy", "Paragraph:after"},
		},
		{
			name:   "a run after a line that goes on the paragraph of a list item in the body",
			source: "- !!! note\n  - item\nlazy\n  !!!\n",
			want: []string{
				"List", "  ListItem", "    !note", "      List", "        ListItem", "          Paragraph:item\nlazy",
			},
		},
		{
			// A run with nothing after it opens nothing, so where it cannot
			// close anything it is text of the paragraph.
			name:   "a run after a run that lacks the list item's indentation",
			source: "- !!! note\n  text\n!!!\n  !!!\nafter\n",
			want:   []string{"List", "  ListItem", "    !note", "      Paragraph:text\n!!!", "Paragraph:after"},
		},
		{
			name:   "a heading without the list item's indentation ends the item, and the admonition with no run",
			source: "- !!! note\n  text\n# heading\n  !!!\n",
			want:   []string{"List", "  ListItem", "    !note", "    Paragraph:text", "Heading:heading", "Paragraph:!!!"},
		},
		{
			name:   "a line without the quote marker after a code block ends the quotation",
			source: "> !!! note\n> ```\n> code\n> ```\ntext\n> !!!\n",
			want:   []string{"Blockquote", "  !note", "  CodeBlock", "Paragraph:text", "Blockquote", "  Paragraph:!!!"},
		},
		{
			name:   "a run after a code block and a line that lacks the list item's indentation",
			source: "- !!! note\n  ```\n  code\n  ```\n  text\nlazy\n  !!!\n",
			want:   []string{"List", "  ListItem", "    !note", "      CodeBlock", "      Paragraph:text\nlazy"},
		},
		{
			// The line level with the inner opening line goes on the inner
			// body's paragraph, and the indented line after it on that body.
			name:   "a nested indented body that goes on after a line level with it, each closed by its run",
			source: "!!! note \"outer\"\nintro\n!!! tip \"inner\"\n    b\nlazy\n\n    c\n!!!\nstill outer\n!!!\nafter\n",
			want: []string{
				"!note", "  Paragraph:intro", "  !tip", "    Paragraph:b\nlazy", "    Paragraph:c", "  Paragraph:still outer",
				"Paragraph:after",
			},
		},
		{
			name:   "a nested indented body that goes on in the same paragraph after a line level with it",
			source: "!!! note \"outer\"\nintro\n!!! tip \"inner\"\n    b\nlazy\n    c\n!!!\nstill outer\n!!!\nafter\n",
			want: []string{
				"!note", "  Paragraph:intro", "  !tip", "    Paragraph:b\nlazy\nc", "  Paragraph:still outer", "Paragraph:after",
			},
		},
		{
			name:   "an indented body of one opened two columns in, which goes on after a line level with it",
			source: "!!! note \"outer\"\ntext\n  !!! tip \"inner\"\n    b\n  lazy\n\n    c\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:text", "  !tip", "    Paragraph:b\nlazy", "    Paragraph:c", "Paragraph:after"},
		},
		{
			// The code block ends with the body it is in, at that body's run.
			name:   "a code block left open in an indented body opened one column in",
			source: "!!! note \"outer\"\ntext\n !!! tip \"inner\"\n   ```\n   code\n !!!\nmore\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:text", "  !tip", "    CodeBlock", "  Paragraph:more", "Paragraph:after"},
		},
		{
			// The comment and the instruction are text of the blocks the
			// tags open, which end at the blank lines.
			name:   "a block of one kind left open, then one of another kind that is closed",
			source: "!!! note \"one\"\n> # h\n<span>\n<!--\n</span>\n\n!!! tip \"two\"\n> # h\n<span>\n<?x\n</span>\n\n?>\n!!!\nafter\n",
			want: []string{
				"!note", "Blockquote", "  Heading:h", "HTMLBlock", "!tip", "  Blockquote", "    Heading:h", "  HTMLBlock",
				"  Paragraph:?>", "Paragraph:after",
			},
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
			name:   "a fenced code block that goes on in a line indented less than the opening line",
			source: "  !!! note \"T\"\n  a\n  ```\ncode\n  ```\n  !!!\nafter\n",
			want:   []string{"!note", "  Paragraph:a", "  CodeBlock", "Paragraph:after"},
		},
		{
			name:   "closed by a run behind a partly taken tab",
			source: "- item\n\n\t!!! note \"x\"\n\tb\n\t!!!\n\tafter\n",
			want: []string{
				"List", "  ListItem", "    Paragraph:item", "    !note", "      Paragraph:b", "    Paragraph:after",
			},
		},
		{
			// A line's column is read off its own tabs: the run is level with
			// the opening line whatever blanks either is written with.
			name:   "in a blockquote behind a tab, closed by a run behind spaces",
			source: ">\t!!! note\n>\tbody\n>   !!!\n\nafter\n",
			want:   []string{"Blockquote", "  !note", "    Paragraph:body", "Paragraph:after"},
		},
		{
			name:   "in a list item behind a tab, closed by a run behind spaces",
			source: "-\t!!! note\n\tbody\n    !!!\n\nafter\n",
			want:   []string{"List", "  ListItem", "    !note", "      Paragraph:body", "Paragraph:after"},
		},
		{
			name:   "in a list item behind spaces, closed by a run behind a tab",
			source: "-   !!! tip\n\n    body\n\t!!!\n\nafter\n",
			want:   []string{"List", "  ListItem", "    !tip", "      Paragraph:body", "Paragraph:after"},
		},
		{
			name:   "in a list item, the body behind a tab, closed by a run behind spaces",
			source: "-   !!! tip\n\n\tbody\n    !!!!\n\nafter\n",
			want:   []string{"List", "  ListItem", "    !tip", "      Paragraph:body", "Paragraph:after"},
		},
		{
			name:   "in an ordered list item behind a tab, closed by a run behind spaces",
			source: "1.\t!!! note\n\tbody\n    !!!\nafter\n",
			want:   []string{"List", "  ListItem", "    !note", "      Paragraph:body", "Paragraph:after"},
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
		{
			// The "{" that is no attribute block starts a paragraph on the
			// opening line, which the next line goes on lazily; the
			// admonition ends with that paragraph.
			name:   "a paragraph on the opening line, then no run",
			source: "!!! note \"Use {name}\"\nnext\n\nafter\n\n## Heading\n",
			want:   []string{"!note", "  Paragraph:{name}\"\nnext", "Paragraph:after", "Heading:Heading"},
		},
		{
			// w4 goes on the paragraph of the inner indented body, and w5
			// on that body, so the first run is the inner one's.
			name: "a run each for three, the inner one indented",
			source: "!!! note \"a\"\nw1\n~~~\nw2\n~~~\n!!! tip \"b\"\n!!! warning \"c\"\n    w3\nw4\n    w5\n" +
				"!!!\n!!!\n!!!\nafter\n",
			want: []string{
				"!note", "  Paragraph:w1", "  CodeBlock", "  !tip", "    !warning", "      Paragraph:w3\nw4\nw5",
				"Paragraph:after",
			},
		},
		{
			// An ordered item numbered other than 1 does not interrupt a
			// paragraph: the fence after it is no item's, and holds the run.
			name:   "a fence after a line that cannot start a list item",
			source: "!!! note \"T\"\nw1\n2. w2\n   ```\n!!!\n   ```\n!!!\nafter\n",
			want:   []string{"!note", "  Paragraph:w1\n2. w2", "  CodeBlock", "Paragraph:after"},
		},
		{
			name:   "a fence after a setext underline",
			source: "!!! note \"T\"\nw1\n-\n  ```\n!!!\n  ```\n!!!\nafter\n",
			want:   []string{"!note", "  Heading:w1", "  CodeBlock", "Paragraph:after"},
		},
		{
			// ">!!!!" is too short for the warning and closes the tip around
			// it; the runs after the second tip's close nothing.
			name: "a run too short for the innermost one, in a blockquote",
			source: "> !!!! tip\n> <![CDATA[\n> ]]>\n>\n> !!!!! warning\n> !!! a\n> \tw1\n>\n> !!!\n>\n>!!!!\n>\n" +
				"> !!! tip\n> w2\nw3\n>\n> !!!\n> !!!!!\n> !!!!\n\nafter\n",
			want: []string{
				"Blockquote", "  !tip", "    HTMLBlock", "    !warning", "    !a", "      Paragraph:w1", "  !tip",
				"    Paragraph:w2\nw3", "  Paragraph:!!!!!\n!!!!", "Paragraph:after",
			},
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
// than each inside the one before, and the run at the end, if any, is read
// against the last of them only.
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

			blocks := outline(t, source)

			assert.Equal(t, []string{"!note", "Paragraph:text", "!note", "Paragraph:text"}, blocks[:4])
			assert.Equal(t, "!note", blocks[2*count-2])

			// Nested, or each looked at for each run, a page this long
			// takes minutes.
			assert.LessOrEqual(t, readAdmonitions(source).work, 2*strings.Count(source, "\n"))
		})
	}
}

// TestAdmonitionStrandedBodyIsRecorded: the parser records, for whoever
// reports it, each admonition that ended at its opening line although the
// line straight after it was written as its body: level with it, and with no
// closing run ahead. An admonition with nothing after it in what holds it,
// with a blank line after it, with an indented body or with a closing run is
// not one.
func TestAdmonitionStrandedBodyIsRecorded(t *testing.T) {
	tests := []struct {
		name   string
		source string
		lines  []int
	}{
		{name: "unindented line straight after", source: "!!! tip\nbody\n\nlater\n", lines: []int{1}},
		{name: "each of two", source: "text\n\n!!! tip\nbody\n\n!!! note \"T\"\nmore\n", lines: []int{3, 6}},
		{name: "in a list item", source: "- one\n- !!! note\n  in the item\n", lines: []int{2}},
		{name: "in a blockquote", source: "> text\n>\n> !!! note\n> quoted\n", lines: []int{3}},
		{
			name:   "the outer of two, the inner indented and closed by the only run",
			source: "!!! note \"outer\"\nintro\n!!! tip \"inner\"\n    b\n!!!\n\nafter\n",
			lines:  []int{1},
		},
		{name: "the outer of two with one run", source: "!!! note\na\n!!! tip\nb\n!!!\nc\n", lines: []int{1}},

		{name: "nothing after it", source: "!!! note \"T\"\n"},
		{name: "blank lines after it", source: "!!! note \"T\"\n\n"},
		{name: "a blank line, then text", source: "!!! note \"T\"\n\ntext\n"},
		{name: "the next list item after it", source: "- !!! note\n- next\n"},
		{name: "indented body", source: "!!! note\n    body\nafter\n"},
		{name: "closed by a run", source: "!!! note\nbody\n!!!\nafter\n"},
		{name: "a run straight after it", source: "!!! note\n!!!\nafter\n"},
		{name: "a line indented less than it", source: "  !!! note\nbody\n"},
		{name: "a line that goes on what the opening line left in it", source: "!!! note {no attributes\nbody\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := parser.NewContext()
			admonitionBlocks.Parse([]byte(tt.source), parser.WithContext(pc))

			var lines []int
			for _, at := range parsenote.StrandedBodies(pc) {
				lines = append(lines, 1+strings.Count(tt.source[:at], "\n"))
			}

			assert.Equal(t, tt.lines, lines)
		})
	}
}
