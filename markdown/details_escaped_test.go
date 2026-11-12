package markdown_test

import (
	"context"
	"testing"

	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A backslash before "<" makes it text: the author is writing about the tag,
// as with `&lt;details&gt;` or a code span, and the page shows it literally.
func TestEscapedDetailsTagIsText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "heading",
			input: "## The \\<details> element\n",
			want:  "The &lt;details&gt; element",
		},
		{
			name:  "paragraph with other characters to escape",
			input: "If 1 < 2 & 3 then \\<details> is \"shown\".\n",
			want:  "<p>If 1 &lt; 2 &amp; 3 then &lt;details&gt; is &quot;shown&quot;.</p>",
		},
		{
			name:  "opening and closing tag",
			input: "Wrap it in \\<details> and \\</details>.\n",
			want:  "<p>Wrap it in &lt;details&gt; and &lt;/details&gt;.</p>",
		},
		{
			name:  "with a summary",
			input: "\\<details>\\<summary>s\\</summary>x\\</details>\n",
			want:  "<p>&lt;details&gt;&lt;summary&gt;s&lt;/summary&gt;x&lt;/details&gt;</p>",
		},
		{
			name:  "table cell",
			input: "| A |\n|---|\n| \\<details> |\n",
			want:  "<td>&lt;details&gt;</td>",
		},
		{
			name:  "list item",
			input: "- use \\<details> here\n",
			want:  "use &lt;details&gt; here",
		},
		{
			name:  "character references",
			input: "Write &lt;details&gt; for it.\n",
			want:  "<p>Write &lt;details&gt; for it.</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			std, err := stdlib.New(context.Background(), nil)
			require.NoError(t, err)

			out, _, err := markdown.CompileMarkdown(t.Context(), []byte(tt.input), std, "test.md", markdown.Options{})
			require.NoError(t, err)

			assert.Contains(t, out, tt.want)
			assert.NotContains(t, out, `ac:name="expand"`, "an escaped tag opens no macro")
			assert.NotContains(t, out, `\`, "the escaping backslash is not shown")
			assertWellFormed(t, out)
		})
	}
}

// An escaped tag beside a real element leaves the element a macro.
func TestEscapedDetailsTagBesideAnElement(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	const input = "- item <details><summary>s</summary>x</details> written as \\<details>\n\n" +
		"<details>\n<summary>Block</summary>\n\nThe \\<details> tag, and 1 < 2.\n\n</details>\n"

	out, _, err := markdown.CompileMarkdown(t.Context(), []byte(input), std, "test.md", markdown.Options{})
	require.NoError(t, err)

	assert.Contains(t, out, `<ac:structured-macro ac:name="expand">`+
		`<ac:parameter ac:name="title">s</ac:parameter>`+
		`<ac:rich-text-body>x</ac:rich-text-body></ac:structured-macro> written as &lt;details&gt;`)
	assert.Contains(t, out, `<ac:parameter ac:name="title">Block</ac:parameter>`)
	assert.Contains(t, out, "<p>The &lt;details&gt; tag, and 1 &lt; 2.</p>")
	assertWellFormed(t, out)
}

// An escaped closing tag inside an element is part of its body, and the
// element ends at its own closing tag.
func TestEscapedClosingTagInsideDetails(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	out, _, err := markdown.CompileMarkdown(t.Context(),
		[]byte("a <details>x \\</details> y</details> b\n"), std, "test.md", markdown.Options{})
	require.NoError(t, err)

	assert.Equal(t, `<p>a <ac:structured-macro ac:name="expand"><ac:rich-text-body>`+
		`x &lt;/details&gt; y</ac:rich-text-body></ac:structured-macro> b</p>`+"\n", out)
	assertWellFormed(t, out)
}

// The inline spellings of a tag goldmark reads as HTML still open a macro.
func TestInlineDetailsTagSpellings(t *testing.T) {
	const want = `<p>a <ac:structured-macro ac:name="expand"><ac:rich-text-body>` +
		`x</ac:rich-text-body></ac:structured-macro> b</p>` + "\n"

	for name, input := range map[string]string{
		"plain":          "a <details>x</details> b\n",
		"upper case":     "a <DETAILS>x</DETAILS> b\n",
		"with attribute": "a <details open>x</details> b\n",
		"over two lines": "a <details\nopen>x</details> b\n",
	} {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(context.Background(), nil)
			require.NoError(t, err)

			out, _, err := markdown.CompileMarkdown(t.Context(), []byte(input), std, "test.md", markdown.Options{})
			require.NoError(t, err)

			assert.Equal(t, want, out)
		})
	}
}
