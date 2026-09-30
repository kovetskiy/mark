package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetailsKeepsStorageFormatBody covers storage-format markup written inside
// a <details>. The body used to be round-tripped through an HTML5 parser, which
// ignores "/>" on anything that is not a void element: <ri:page .../> became an
// open element and the siblings after it were moved inside it. The result was
// still well-formed, so nothing objected, and the page published a link with no
// body and an emoticon holding the paragraph's text.
func TestDetailsKeepsStorageFormatBody(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "self-closed page reference in a link",
			input: "<details><summary>S</summary>\n" +
				`<ac:link><ri:page ri:content-title="Other"/><ac:plain-text-link-body><![CDATA[go]]></ac:plain-text-link-body></ac:link>` +
				"\n</details>\n",
			want: `<ac:link><ri:page ri:content-title="Other"/><ac:plain-text-link-body><![CDATA[go]]></ac:plain-text-link-body></ac:link>`,
		},
		{
			name:  "self-closed emoticon before text",
			input: "<details><summary>S</summary>\n<p><ac:emoticon ac:name=\"smile\"/> hello world</p>\n</details>\n",
			want:  `<p><ac:emoticon ac:name="smile"/> hello world</p>`,
		},
		{
			name: "self-closed user and attachment references",
			input: "<details><summary>S</summary>\n" +
				`<p><ac:link><ri:user ri:account-id="42"/></ac:link> and <ac:image><ri:attachment ri:filename="a.png"/></ac:image> end</p>` +
				"\n</details>\n",
			want: `<p><ac:link><ri:user ri:account-id="42"/></ac:link> and <ac:image><ri:attachment ri:filename="a.png"/></ac:image> end</p>`,
		},
		{
			name:  "table is not given a tbody",
			input: "<details><summary>S</summary>\n<table><tr><td>a</td></tr></table>\n</details>\n",
			want:  `<table><tr><td>a</td></tr></table>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, compile := range compilers {
				out, _, err := compile([]byte(tt.input), lib, "testdata/test.md", types.MarkConfig{})
				require.NoError(t, err, name)

				assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">S</ac:parameter><ac:rich-text-body>`, name)
				assert.Contains(t, out, tt.want, name)
				assert.NoError(t, CheckWellFormed(out), name)
			}
		})
	}
}

// TestDetailsSummaryAfterContent covers a <summary> that is not the first thing
// in its <details>. A browser takes it as the title wherever it sits among the
// element's children, and it must not be left in the body as markup
// Confluence has no meaning for.
func TestDetailsSummaryAfterContent(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := CompileMarkdown(
		[]byte("<details>\n<p>pre</p><summary>Late</summary>\n<p>x</p>\n</details>\n"),
		lib, "testdata/test.md", types.MarkConfig{},
	)
	require.NoError(t, err)

	assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Late</ac:parameter><ac:rich-text-body>`)
	assert.Contains(t, out, "<p>pre</p>")
	assert.Contains(t, out, "<p>x</p>")
	assert.NotContains(t, out, "summary")
}

// TestDetailsBodyWithHTMLOnlyMarkupIsRepaired: a details body is copied as
// written, so what only HTML accepts -- a bare ampersand, an entity XML has no
// name for, an unquoted attribute -- reaches the page unless the repair of raw
// HTML covers it there too. html.Parse used to fix these on the way through;
// without either, the whole page is refused.
func TestDetailsBodyWithHTMLOnlyMarkupIsRepaired(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	input := "<details><summary>S</summary>\n" +
		`<p>AT&T <a href="?a=1&b=2">x</a> &NotEqualTilde;</p><table><tr><td colspan=2>x</td></tr></table>` +
		"\n</details>\n"

	for name, compile := range compilers {
		out, _, err := compile([]byte(input), lib, "testdata/test.md", types.MarkConfig{})
		require.NoError(t, err, name)
		require.NoError(t, CheckWellFormed(out), name)
	}
}
