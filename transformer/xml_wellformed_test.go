package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"input", `<input type="checkbox" checked>`, `<input type="checkbox" checked="checked" />`, true},
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

// TestCDATAIsCopiedThroughUntouched: html.NewTokenizer has no notion of CDATA.
// It reports "<![CDATA[" as a bogus comment ending at the first ">" and reads
// the rest as markup, so a code sample containing "<br>" had it rewritten to
// "<br />" -- the one thing a CDATA section exists to prevent, done by the pass
// that was added to make the page well-formed.
func TestCDATAIsCopiedThroughUntouched(t *testing.T) {
	samples := []string{
		`<ac:plain-text-body><![CDATA[if a > b then <br> done]]></ac:plain-text-body>`,
		`<ac:plain-text-body><![CDATA[<input type="checkbox">]]></ac:plain-text-body>`,
		`<ac:plain-text-body><![CDATA[a <!-- b -- c --> d]]></ac:plain-text-body>`,
	}

	for _, sample := range samples {
		out, changed := wellFormedHTML([]byte(sample))

		assert.False(t, changed, "nothing inside CDATA needs repairing: %s", sample)
		assert.Equal(t, sample, string(out))
	}
}

// TestMarkupAroundCDATAIsStillRepaired is the other half: cutting the sections
// out must not stop the rest of the block being fixed.
func TestMarkupAroundCDATAIsStillRepaired(t *testing.T) {
	out, changed := wellFormedHTML([]byte(
		`<div><br><ac:plain-text-body><![CDATA[keep <br> me]]></ac:plain-text-body><hr></div>`,
	))

	require.True(t, changed)
	assert.Contains(t, string(out), `<div><br />`, "markup before the section is repaired")
	assert.Contains(t, string(out), `<![CDATA[keep <br> me]]>`, "the section is not")
	assert.Contains(t, string(out), `<hr /></div>`, "markup after it is repaired")
}

// TestUnterminatedCDATAIsLeftAlone: an opener with no closer means everything
// after it is inside the section as far as anything downstream can tell.
func TestUnterminatedCDATAIsLeftAlone(t *testing.T) {
	const sample = `<ac:plain-text-body><![CDATA[unterminated <br>`

	out, changed := wellFormedHTML([]byte(sample))

	assert.False(t, changed)
	assert.Equal(t, sample, string(out))
}

// HTML reads a "&" that starts no reference as itself, so a query string or a
// company name written in raw HTML renders in a browser and leaves the page
// not well-formed XML, which Confluence rejects in its entirety.
func TestWellFormedHTMLEscapesBareAmpersands(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		raw      string
		expected string
		changed  bool
	}{
		{"in an attribute", `<a href="https://x.com/?a=1&b=2">`, `<a href="https://x.com/?a=1&amp;b=2">`, true},
		{"in text", "<div>AT&T</div>", "<div>AT&amp;T</div>", true},
		{"alone", "<p>a & b</p>", "<p>a &amp; b</p>", true},
		{"at the end", "<p>R&</p>", "<p>R&amp;</p>", true},
		{"no semicolon", "<p>&copy 2024</p>", "<p>&amp;copy 2024</p>", true},
		{"named reference kept", "<p>&amp; &nbsp; &copy; &apos;</p>", "<p>&amp; &nbsp; &copy; &apos;</p>", false},
		{"decimal reference kept", "<p>&#160; &#27;</p>", "<p>&#160; &#27;</p>", false},
		{"hex reference kept", "<p>&#xA0; &#X1b;</p>", "<p>&#xA0; &#X1b;</p>", false},
		{"not a number", "<p>&#xZZ;</p>", "<p>&amp;#xZZ;</p>", true},
		{"HTML5-only name", "<p>&NotEqualTilde;</p>", "<p>\u2242\u0338</p>", true},
		{"HTML5-only name for markup", "<p>&LT;</p>", "<p>&lt;</p>", true},
		{"escaped already", `<a href="?a=1&amp;b=2">`, `<a href="?a=1&amp;b=2">`, false},
		{"comment untouched", "<!-- AT&T -->", "<!-- AT&T -->", false},
		{"cdata untouched", "<![CDATA[AT&T]]>", "<![CDATA[AT&T]]>", false},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			out, changed := wellFormedHTML([]byte(testcase.raw))

			assert.Equal(t, testcase.expected, string(out))
			assert.Equal(t, testcase.changed, changed)
		})
	}
}

// HTML allows an attribute value with no quotes around it, or no value at
// all; XML allows neither.
func TestWellFormedHTMLQuotesAttributeValues(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		raw      string
		expected string
		changed  bool
	}{
		{"unquoted", "<p align=center>", `<p align="center">`, true},
		{"unquoted among quoted", `<td class="x" colspan=2 id='y'>`, `<td class="x" colspan="2" id='y'>`, true},
		{"unquoted holding an ampersand", "<a href=?a=1&b=2>", `<a href="?a=1&amp;b=2">`, true},
		{"unquoted holding a quote", `<p title=a"b>`, `<p title="a&quot;b">`, true},
		{"space around the equals", "<p align = center>", `<p align = "center">`, true},
		{"no value", "<td nowrap>", `<td nowrap="nowrap">`, true},
		{"no value on a void element", "<input disabled>", `<input disabled="disabled" />`, true},
		{"self-closing", "<ac:emoticon ac:name=smile/>", `<ac:emoticon ac:name="smile/">`, true},
		{"self-closing quoted", `<ac:emoticon ac:name="smile" />`, `<ac:emoticon ac:name="smile" />`, false},
		{"bracket in a quoted value", `<p title="a<b">`, `<p title="a&lt;b">`, true},
		{"quoted untouched", `<p align="center" title='it"s'>`, `<p align="center" title='it"s'>`, false},
		{"end tag untouched", "</p>", "</p>", false},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			out, changed := wellFormedHTML([]byte(testcase.raw))

			assert.Equal(t, testcase.expected, string(out))
			assert.Equal(t, testcase.changed, changed)
		})
	}
}
