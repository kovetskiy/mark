package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloseOmittedEndTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"cells", `<table><tr><td>a<td>b</table>`, `<table><tr><td>a</td><td>b</td></tr></table>`},
		{"rows", `<table><tr><td>a<tr><td>b</table>`, `<table><tr><td>a</td></tr><tr><td>b</td></tr></table>`},
		{"paragraphs", `<div><p>one<p>two</div>`, `<div><p>one</p><p>two</p></div>`},
		{"block ends a paragraph", `<p>one<ul><li>a</ul>`, `<p>one</p><ul><li>a</li></ul>`},
		{"list items", `<ul><li>a<li>b</ul>`, `<ul><li>a</li><li>b</li></ul>`},
		{"nested list", `<ul><li>a<ul><li>b</ul><li>c</ul>`, `<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>`},
		{"definition list", `<dl><dt>a<dd>b<dt>c</dl>`, `<dl><dt>a</dt><dd>b</dd><dt>c</dt></dl>`},
		{"case is kept", `<TABLE><TR><TD>a<TD>b</TABLE>`, `<TABLE><TR><TD>a</TD><TD>b</TD></TR></TABLE>`},
		{"paragraph in a cell does not end the list", `<ul><li><table><tr><td><p>x<li>y`, `<ul><li><table><tr><td><p>x<li>y`},
		{
			"storage format is not closed",
			`<ac:rich-text-body><p><ac:link><ri:page ri:content-title="x"/></ac:link><p>b</ac:rich-text-body>`,
			`<ac:rich-text-body><p><ac:link><ri:page ri:content-title="x"/></ac:link></p><p>b</p></ac:rich-text-body>`,
		},
		{"required end tag is left alone", `<p><b>x</p>`, `<p><b>x</p>`},
		{"open at the end stays open", `<ul><li>a`, `<ul><li>a`},
		{"void elements open nothing", `<td>a<br><td>b</table>`, `<td>a<br></td><td>b</table>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := closeOmittedEndTags([]byte(tt.in))
			assert.Equal(t, tt.want, string(got))
			assert.Equal(t, tt.in != tt.want, changed)
		})
	}
}
