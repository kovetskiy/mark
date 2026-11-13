package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConvertLineBreakEndingARun: a line break that ends emphasis or a link
// is written after the closing delimiter, where its backslash ends the line
// rather than escaping the delimiter.
func TestConvertLineBreakEndingARun(t *testing.T) {
	for _, test := range []struct {
		storage, markdown, published string
	}{
		{
			storage:   `<p><strong>bold<br/></strong>next</p>`,
			markdown:  "**bold**\\\nnext\n",
			published: `<p><strong>bold</strong><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><em>slanted<br/></em>next</p>`,
			markdown:  "*slanted*\\\nnext\n",
			published: `<p><em>slanted</em><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><s>struck<br/></s>next</p>`,
			markdown:  "~~struck~~\\\nnext\n",
			published: `<p><del>struck</del><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><a href="https://example.com/">text<br/></a>next</p>`,
			markdown:  "[text](https://example.com/)\\\nnext\n",
			published: `<p><a href="https://example.com/">text</a><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><strong>very <em>both<br/></em></strong>next</p>`,
			markdown:  "**very *both***\\\nnext\n",
			published: `<p><strong>very <em>both</em></strong><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><strong>bold <br/> <br/></strong>next</p>`,
			markdown:  "**bold**\\\n\\\nnext\n",
			published: `<p><strong>bold</strong><br/>` + "\n" + `<br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p><strong>bold.<br/></strong>next</p>`,
			markdown:  "**bold.**\\\nnext\n",
			published: `<p><strong>bold.</strong><br/>` + "\n" + `next</p>`,
		},
		{
			storage:   `<p>a <strong><br/></strong>b</p>`,
			markdown:  "a \\\nb\n",
			published: `<p>a <br/>` + "\n" + `b</p>`,
		},
		{
			// At the end of the paragraph the break breaks nothing.
			storage:   `<p><strong>bold<br/></strong></p>`,
			markdown:  "**bold**\n",
			published: `<p><strong>bold</strong></p>`,
		},
		{
			storage:   `<p><strong>bold<br/><br/></strong></p>`,
			markdown:  "**bold**\n",
			published: `<p><strong>bold</strong></p>`,
		},
		{
			storage:   `<p>plain<br/> <br/></p>`,
			markdown:  "plain\n",
			published: `<p>plain</p>`,
		},
		{
			// Space after the break, from an empty run, does not keep it.
			storage:   `<p>plain<br/><strong> </strong></p>`,
			markdown:  "plain\n",
			published: `<p>plain</p>`,
		},
		{
			storage:   `<p><code>code</code><br/><em> </em><br/></p>`,
			markdown:  "`code`\n",
			published: `<p><code>code</code></p>`,
		},
		{
			storage:   `<p>plain<br/><strong> </strong>next</p>`,
			markdown:  "plain\\\nnext\n",
			published: `<p>plain<br/>` + "\n" + `next</p>`,
		},
		{
			// A backslash the text ends in is text, not a break.
			storage:   `<p><strong>a\</strong> b</p>`,
			markdown:  "**a\\\\** b\n",
			published: `<p><strong>a\</strong> b</p>`,
		},
		{
			// A break inside the run stays where it is.
			storage:   `<p><strong>one<br/>two</strong> next</p>`,
			markdown:  "<strong>one\\\ntwo</strong> next\n",
			published: `<p><strong>one<br/>` + "\n" + `two</strong> next</p>`,
		},
		{
			storage:   `<p><a href="https://example.com/">one<br/>two</a> next</p>`,
			markdown:  "[one\\\ntwo](https://example.com/) next\n",
			published: `<p><a href="https://example.com/">one<br/>` + "\n" + `two</a> next</p>`,
		},
		{
			// A table cell writes a break as <br/>, which escapes nothing.
			storage:   `<table><tbody><tr><th>a</th></tr><tr><td><strong>bold<br/></strong>next</td></tr></tbody></table>`,
			markdown:  "| a |\n| --- |\n| <strong>bold<br/></strong>next |\n",
			published: `<table><thead><tr><th>a</th></tr></thead><tbody><tr><td><strong>bold<br/></strong>next</td></tr></tbody></table>`,
		},
	} {
		md, published := republish(t, test.storage)
		assert.Equal(t, test.markdown, md, test.storage)
		assert.Equal(t, canonical(t, test.published), published, test.storage)
	}
}
