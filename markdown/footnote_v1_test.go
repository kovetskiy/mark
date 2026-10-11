package mark_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
)

type footnoteV1Case struct {
	name     string
	markdown string
	want     string
}

// assertFootnotesAsV1 compiles each case through both compile paths, with the
// default features and with the parser features on, and expects the body
// goldmark v1 produced, byte for byte.
func assertFootnotesAsV1(t *testing.T, cases []footnoteV1Case) {
	t.Helper()

	lib, err := stdlib.New(nil)
	assert.NoError(t, err)

	compilers := []struct {
		name    string
		compile func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error)
	}{
		{"default", mark.CompileMarkdown},
		{"legacy", mark.CompileMarkdownLegacy},
	}

	featureSets := []struct {
		name     string
		features []string
	}{
		{"default features", nil},
		{"extended features", []string{"emoji", "mkdocsadmonitions", "date", "mention", "inline-link-card"}},
	}

	for _, tc := range cases {
		for _, compiler := range compilers {
			for _, featureSet := range featureSets {
				t.Run(tc.name+"/"+compiler.name+"/"+featureSet.name, func(t *testing.T) {
					actual, _, err := compiler.compile([]byte(tc.markdown), lib, "testdata/test.md", types.MarkConfig{
						Features: featureSet.features,
					})
					assert.NoError(t, err)
					assert.Equal(t, tc.want, actual)
				})
			}
		}
	}
}

// TestFootnoteNumberingMatchesV1 pins the numbers goldmark v1 gave the notes.
// v1 numbered a note at its first citation as it parsed the inlines, which it
// did only after every definition had been gathered into one list where the
// first definition stood, so the citations inside the notes count from there.
func TestFootnoteNumberingMatchesV1(t *testing.T) {
	assertFootnotesAsV1(t, []footnoteV1Case{
		{
			name:     "definition first, cited from a later definition",
			markdown: "[^b]: B note\n\nText [^a] and [^b].\n\n[^a]: A cites [^c]\n\n[^c]: C note\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link> and <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-3</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-3"><ac:link-body><sup>[3]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>C note&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>A cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-3</ac:parameter></ac:structured-macro>
<p>B note&#160;<ac:link ac:anchor="footnote-ref-3"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition first",
			markdown: "[^a]: A note\n\nText [^b] and [^a].\n\n[^b]: B note\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link> and <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>B note&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>A note&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "citations nested in definitions",
			markdown: "Text [^a].\n\n[^a]: A cites [^b] and [^c]\n\n[^b]: B note\n[^c]: C cites [^b]\n\nMore [^c].\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<p>More <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-3-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-3"><ac:link-body><sup>[3]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>A cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link> and <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-3</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-3"><ac:link-body><sup>[3]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>B note&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-3</ac:parameter></ac:structured-macro>
<p>C cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-3"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-3-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "repeated citations",
			markdown: "[^x]: X cites [^y]\n\nOne [^y] two [^x] three [^y] four [^x].\n\n[^y]: Y\n",
			want: `<p>One <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link> two <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link> three <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link> four <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>Y&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-1-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-1-2"><ac:link-body>&#x21a9;&#xfe0e;<sup>3</sup></ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>X cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "uncited definitions",
			markdown: "[^u]: unused cites [^a]\n\nText [^a].\n\n[^a]: A\n[^v]: also unused\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>A&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-1-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "citations in lists",
			markdown: "- one [^a]\n- two [^b]\n\n[^b]: B cites [^c]\n\n- three [^c]\n\n[^a]: A\n[^c]: C\n",
			want: `<ul>
<li>one <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link></li>
<li>two <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link></li>
</ul>
<ul>
<li>three <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-3-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-3"><ac:link-body><sup>[3]</sup></ac:link-body></ac:link></li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>A&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>B cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-3</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-3"><ac:link-body><sup>[3]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-3</ac:parameter></ac:structured-macro>
<p>C&#160;<ac:link ac:anchor="footnote-ref-3"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-3-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "citations in a blockquote",
			markdown: "> quoted [^q]\n> [^q]: Q in quote cites [^r]\n\nText [^r].\n\n[^r]: R\n",
			want: `<blockquote>
<p>quoted <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link></p>
</blockquote>
<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>.</p>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>Q in quote cites <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>R&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;<sup>1</sup></ac:link-body></ac:link>&#160;<ac:link ac:anchor="footnote-ref-2-1"><ac:link-body>&#x21a9;&#xfe0e;<sup>2</sup></ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
	})
}
