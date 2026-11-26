package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// '???' becomes an expand around the panel and '???+' the panel alone.
func TestCollapsedAdmonition(t *testing.T) {
	const doc = "??? tip \"Fold & hide\"\n    Hidden.\n???\n\n???+ note \"Shown\"\n    Visible.\n"

	out := compileDoc(t, doc, "mkdocsadmonitions")

	assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Fold &amp; hide</ac:parameter>`)
	assert.Contains(t, out, `<ac:structured-macro ac:name="tip">`)
	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
	assert.Equal(t, 1, strings.Count(out, `ac:name="expand"`))
}

// A '???' title runs to the last quote on its line, as in MkDocs, so it may
// hold quotes or end in a backslash.
func TestCollapsedAdmonitionTitleRunsToTheLastQuote(t *testing.T) {
	tests := []struct {
		source, title string
	}{
		{"??? note \"C:\\\"\n    body\n", `C:\`},
		{"??? note \"say \"hi\" now\"\n    body\n", `say &#34;hi&#34; now`},
		{"??? note \"say \"hi\"\" {.x}\n    body\n", `say &#34;hi&#34;`},
		{"??? note \"a & <b>\" \n    body\n", `a &amp; &lt;b&gt;`},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			out := compileDoc(t, tt.source, "mkdocsadmonitions")

			assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">`+tt.title+`</ac:parameter>`)
			assert.Contains(t, out, "<p><strong>"+tt.title+"</strong></p>\n<p>body</p>")
			assert.True(t, wellFormed(t, out), out)
		})
	}
}
