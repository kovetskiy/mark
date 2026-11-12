package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A macro parameter is a string Confluence reads as it stands, so the
// characters Markdown gives a meaning to are published as written.
func TestMacroParameterIsLiteral(t *testing.T) {
	t.Run("a built-in template's parameter", func(t *testing.T) {
		out := compileDoc(t, "<!-- Macro: :toc:\n"+
			"     Template: ac:toc\n"+
			"     Include: '.*Guide.*'\n"+
			"     Exclude: 'Draft\\.Notes'\n"+
			"     -->\n\n"+
			":toc:\n")

		assert.Contains(t, out, `<ac:parameter ac:name="include">.*Guide.*</ac:parameter>`)
		assert.Contains(t, out, `<ac:parameter ac:name="exclude">Draft\.Notes</ac:parameter>`)
	})

	tests := map[string]struct {
		value string
		want  string
	}{
		"emphasis and strong": {
			value: `title ~ "*foo*" and text ~ "__init__"`,
			want:  `title ~ &quot;*foo*&quot; and text ~ &quot;__init__&quot;`,
		},
		"a URL": {
			value: `https://example.com/a_b_c`,
			want:  `https://example.com/a_b_c`,
		},
		"an autolink": {
			value: `<https://example.com>`,
			want:  `&lt;https://example.com&gt;`,
		},
		"a code span": {
			value: "a `b` c",
			want:  "a `b` c",
		},
		"a backslash": {
			value: `C:\*.txt \[x\]`,
			want:  `C:\*.txt \[x\]`,
		},
		"a link": {
			value: `[x](https://example.com)`,
			want:  `[x](https://example.com)`,
		},
		"an ampersand": {
			value: `a & b`,
			want:  `a &amp; b`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			out := compileDoc(t, "<!-- Macro: :q:\n"+
				"     Template: #q\n"+
				"     Value: '"+tt.value+"'\n"+
				"     q: '<ac:structured-macro ac:name=\"x\">"+
				"<ac:parameter ac:name=\"v\">{{ .Value | xmlesc }}</ac:parameter>"+
				"</ac:structured-macro>' -->\n\n"+
				"before :q: after\n")

			assert.Contains(t, out, `<ac:parameter ac:name="v">`+tt.want+`</ac:parameter>`)
		})
	}

	t.Run("written in the document", func(t *testing.T) {
		out := compileDoc(t, `<ac:structured-macro ac:name="x">`+
			`<ac:parameter ac:name="v">a_b_ *c* &amp; &copy; &#34;d&#34; &unknown; x < y</ac:parameter>`+
			`<ac:default-parameter>*e*</ac:default-parameter>`+
			`</ac:structured-macro>`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v">a_b_ *c* &amp; © &quot;d&quot; &amp;unknown; x &lt; y</ac:parameter>`)
		assert.Contains(t, out, `<ac:default-parameter>*e*</ac:default-parameter>`)
	})

	t.Run("over two lines", func(t *testing.T) {
		out := compileDoc(t, `<ac:structured-macro ac:name="x"><ac:parameter ac:name="v">*a`+"\n"+
			`b*</ac:parameter></ac:structured-macro>`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v">*a`+"\n"+`b*</ac:parameter>`)
	})

	t.Run("in a table cell", func(t *testing.T) {
		out := compileDoc(t, "| a |\n| --- |\n"+
			`| <ac:structured-macro ac:name="x"><ac:parameter ac:name="v">*a*</ac:parameter></ac:structured-macro> |`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v">*a*</ac:parameter>`)
	})

	// A cell cannot hold a pipe unless it is written "\|", and the table has
	// read that as a pipe before the parameter is.
	t.Run("a pipe in a table cell", func(t *testing.T) {
		out := compileDoc(t, "| a |\n| --- |\n"+
			`| <ac:structured-macro ac:name="x"><ac:parameter ac:name="v">Guide\|*Notes* C:\x</ac:parameter></ac:structured-macro> y\|z |`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v">Guide|*Notes* C:\x</ac:parameter></ac:structured-macro> y|z</td>`)
	})

	t.Run("a pipe outside a table", func(t *testing.T) {
		out := compileDoc(t, `<ac:structured-macro ac:name="x"><ac:parameter ac:name="v">Guide\|Notes</ac:parameter></ac:structured-macro>`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v">Guide\|Notes</ac:parameter>`)
	})
}

// What a parameter holds besides text goes out as it did: an element, a CDATA
// section, and the Markdown of a macro's body beside it.
func TestMacroParameterKeepsItsElements(t *testing.T) {
	t.Run("an element", func(t *testing.T) {
		const parameter = `<ac:parameter ac:name="page"><ac:link><ri:page ri:content-title="a*b*c &amp; d"/></ac:link></ac:parameter>`

		out := compileDoc(t, `<ac:structured-macro ac:name="children">`+parameter+`</ac:structured-macro>`+"\n")

		assert.Contains(t, out, parameter)
	})

	t.Run("a CDATA section", func(t *testing.T) {
		const parameter = `<ac:parameter ac:name="v"><![CDATA[a </ac:parameter> & *b*]]></ac:parameter>`

		out := compileDoc(t, `<ac:structured-macro ac:name="x">`+parameter+`</ac:structured-macro>`+"\n")

		assert.Contains(t, out, parameter)
	})

	t.Run("a rich text body", func(t *testing.T) {
		out := compileDoc(t, `<ac:structured-macro ac:name="info">`+
			`<ac:parameter ac:name="title">*a*</ac:parameter>`+
			`<ac:rich-text-body>*b* and `+"`c`"+`</ac:rich-text-body>`+
			`</ac:structured-macro>`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="title">*a*</ac:parameter>`)
		assert.Contains(t, out, `<ac:rich-text-body><em>b</em> and <code>c</code></ac:rich-text-body>`)
	})

	t.Run("a parameter that is not closed", func(t *testing.T) {
		out := compileDoc(t, `<ac:parameter ac:name="v">*a*`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v"><em>a</em>`)
	})

	t.Run("an empty parameter", func(t *testing.T) {
		out := compileDoc(t, `<ac:structured-macro ac:name="x"><ac:parameter ac:name="v"/>*a*<ac:parameter ac:name="w"></ac:parameter></ac:structured-macro>`+"\n")

		assert.Contains(t, out, `<ac:parameter ac:name="v"/><em>a</em><ac:parameter ac:name="w"></ac:parameter>`)
	})
}
