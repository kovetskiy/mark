package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAdmonitionWithoutAnIndentedBodyIsItsTitle: what follows an admonition
// with no indented body and no closing run is published after the box, not
// in it.
func TestAdmonitionWithoutAnIndentedBodyIsItsTitle(t *testing.T) {
	const box = `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>` + "\n" +
		`<p><strong>Remember</strong></p>` + "\n" +
		`</ac:rich-text-body></ac:structured-macro>` + "\n"

	for name, source := range map[string]string{
		"blank line":      "!!! note \"Remember\"\n\nNext paragraph.\n\n## Heading\n",
		"unindented line": "!!! note \"Remember\"\nNext paragraph.\n\n## Heading\n",
	} {
		t.Run(name, func(t *testing.T) {
			out := compileDoc(t, source, "mkdocsadmonitions")

			after, found := strings.CutPrefix(out, box)
			if assert.True(t, found, "the box holds its title only: %s", out) {
				assert.Contains(t, after, "<p>Next paragraph.</p>")
				assert.Contains(t, after, "Heading</h2>")
			}
		})
	}
}

// TestAdmonitionUnindentedBodyUpToAClosingRun: a closing run keeps the lines
// before it in the box, indented or not.
func TestAdmonitionUnindentedBodyUpToAClosingRun(t *testing.T) {
	out := compileDoc(t, "!!! note \"Remember\"\nIn the box.\n!!!\n\nAfter the box.\n", "mkdocsadmonitions")

	assert.Equal(t, `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>`+"\n"+
		`<p><strong>Remember</strong></p>`+"\n"+
		`<p>In the box.</p>`+"\n"+
		`</ac:rich-text-body></ac:structured-macro>`+"\n"+
		`<p>After the box.</p>`+"\n", out)
}

// TestAdmonitionRunInsideLiteralText: a line of "!!!" inside a code block or
// an HTML block is that block's text, whether or not an admonition is open
// around it.
func TestAdmonitionRunInsideLiteralText(t *testing.T) {
	const title = `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>` + "\n" +
		`<p><strong>Remember</strong></p>` + "\n"
	const end = `</ac:rich-text-body></ac:structured-macro>` + "\n"

	for name, tt := range map[string]struct{ source, want string }{
		"HTML block after a title": {
			source: "!!! note \"Remember\"\n\ntext\n\n<pre>\n!!!\n</pre>\n\nend\n",
			want:   title + end + "<p>text</p>\n<pre>\n!!!\n</pre>\n<p>end</p>\n",
		},
		"HTML block indented a little, after a title": {
			source: "!!! note \"Remember\"\ntext\n <pre>\n!!!\n </pre>\n\nend\n",
			want:   title + end + "<p>text</p>\n <pre>\n!!!\n </pre>\n<p>end</p>\n",
		},
		"HTML block in a closed body": {
			source: "!!! note \"Remember\"\n<pre>\n!!!\n</pre>\n!!!\nend\n",
			want:   title + "<pre>\n!!!\n</pre>\n" + end + "<p>end</p>\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, compileDoc(t, tt.source, "mkdocsadmonitions"))
		})
	}

	out := compileDoc(t, "!!! note \"Remember\"\n\nOne.\n\n```text\n!!! note \"T\"\nbody\n!!!\n```\n\nRest.\n", "mkdocsadmonitions")

	after, found := strings.CutPrefix(out, title+end+"<p>One.</p>\n")
	if assert.True(t, found, out) {
		assert.Contains(t, after, "<![CDATA[!!! note \"T\"\nbody\n!!!]]>")
		assert.True(t, strings.HasSuffix(after, "<p>Rest.</p>\n"), after)
	}
}

// TestAdmonitionWithoutABodyOrATitle: with no body, what is published is what
// the opening line alone gives: an empty box without a title, and an empty
// quotation for a type that has no box, whose title is not published.
func TestAdmonitionWithoutABodyOrATitle(t *testing.T) {
	assert.Equal(t, `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>`+"\n"+
		`</ac:rich-text-body></ac:structured-macro>`+"\n"+
		`<p>text</p>`+"\n", compileDoc(t, "!!! note\n\ntext\n", "mkdocsadmonitions"))

	assert.Equal(t, `<blockquote class="admonition adm-example"></blockquote>`+"\n"+
		`<p>text</p>`+"\n", compileDoc(t, "!!! example \"Usage\"\n\ntext\n", "mkdocsadmonitions"))
}

// TestAdmonitionClosingRunAfterHTML: a closing run straight after an HTML
// block that ends at a blank line closes the admonition, and is not published
// as that block's text.
func TestAdmonitionClosingRunAfterHTML(t *testing.T) {
	out := compileDoc(t, "!!! note \"Remember\"\nIn the box.\n<div>also</div>\n!!!\n\nAfter the box.\n", "mkdocsadmonitions")

	assert.Equal(t, `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>`+"\n"+
		`<p><strong>Remember</strong></p>`+"\n"+
		`<p>In the box.</p>`+"\n"+
		`<div>also</div>`+"\n"+
		`</ac:rich-text-body></ac:structured-macro>`+"\n"+
		`<p>After the box.</p>`+"\n", out)
}

// TestAdmonitionsEachClosedByARun: of two admonitions, one in the other, each
// ends at its own closing run, whether or not the inner body is indented.
func TestAdmonitionsEachClosedByARun(t *testing.T) {
	const want = `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>` + "\n" +
		`<p><strong>Outer</strong></p>` + "\n" +
		`<p>a</p>` + "\n" +
		`<ac:structured-macro ac:name="tip"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>` + "\n" +
		`<p><strong>Inner</strong></p>` + "\n" +
		`<p>b</p>` + "\n" +
		`</ac:rich-text-body></ac:structured-macro>` + "\n" +
		`<p>c</p>` + "\n" +
		`</ac:rich-text-body></ac:structured-macro>` + "\n" +
		`<p>after</p>` + "\n"

	for name, body := range map[string]string{"indented": "    b", "level": "b"} {
		t.Run(name, func(t *testing.T) {
			source := "!!! note \"Outer\"\na\n!!! tip \"Inner\"\n" + body + "\n!!!\nc\n!!!\nafter\n"

			assert.Equal(t, want, compileDoc(t, source, "mkdocsadmonitions"))
		})
	}
}
