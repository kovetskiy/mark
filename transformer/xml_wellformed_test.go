package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Void elements written the HTML way leave the page as not-well-formed XML,
// and Confluence rejects the whole page rather than the element. `<br>` inside
// a table cell is the standard Markdown idiom for a multi-line cell, so this
// took out documents that were otherwise plain Markdown.
func TestWellFormedHTMLClosesVoidElements(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		raw      string
		expected string
		changed  bool
	}{
		{"br", "1. one<br>2. two", "1. one<br />2. two", true},
		{"hr", "<hr>\n", "<hr />\n", true},
		{"input", `<input type="checkbox" checked>`, `<input type="checkbox" checked />`, true},
		{"img with attributes", `<img src="a.png" width="10">`, `<img src="a.png" width="10" />`, true},
		{"already self-closed", "<br />", "<br />", false},
		{"self-closed without space", "<br/>", "<br/>", false},
		{"non-void element untouched", "<b>bold</b>", "<b>bold</b>", false},
		{"confluence markup untouched", `<ac:emoticon ac:name="smile"/>`, `<ac:emoticon ac:name="smile"/>`, false},
		{"attribute holding a bracket", `<img alt="a>b" src="x">`, `<img alt="a>b" src="x" />`, true},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			out, changed := wellFormedHTML([]byte(testcase.raw))

			assert.Equal(t, testcase.expected, string(out))
			assert.Equal(t, testcase.changed, changed)
		})
	}
}

// `--` is illegal anywhere in an XML comment body, so a comment an author wrote
// as a note to themselves rejected the page it was written in.
func TestWellFormedHTMLRepairsCommentBodies(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		raw      string
		expected string
		changed  bool
	}{
		{"double hyphen", "<!-- TODO -- revisit this later -->", "<!-- TODO - revisit this later -->", true},
		{"long run", "<!-- a ----- b -->", "<!-- a - b -->", true},
		{"trailing hyphen", "<!-- note- -->", "<!-- note- -->", false},
		{"body ending in hyphen", "<!-- note --->", "<!-- note -->", true},
		{"plain comment untouched", "<!-- Info -->", "<!-- Info -->", false},
		{"single hyphens untouched", "<!-- ac:layout-section type:single -->", "<!-- ac:layout-section type:single -->", false},
		// The tokenizer reports a CDATA section as a bogus comment; stdlib
		// templates emit those on purpose and they are not comments at all.
		{"cdata untouched", "<![CDATA[a -- b]]>", "<![CDATA[a -- b]]>", false},
		{"unterminated comment untouched", "<!-- a -- b", "<!-- a -- b", false},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			out, changed := wellFormedHTML([]byte(testcase.raw))

			assert.Equal(t, testcase.expected, string(out))
			assert.Equal(t, testcase.changed, changed)
		})
	}
}
