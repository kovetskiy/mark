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
// existing pages were published with, byte for byte.
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

// TestFootnoteNumberingMatchesV1 pins the numbers the notes get: in order of
// first citation, with the citations inside the notes counted from where the
// first definition stands.
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

// TestFootnoteListTightnessMatchesV1 pins whether a list holding a footnote
// definition is tight: a definition alone never makes a list loose.
func TestFootnoteListTightnessMatchesV1(t *testing.T) {
	assertFootnotesAsV1(t, []footnoteV1Case{
		{
			name:     "definition after a blank line is the only extra block",
			markdown: "Text [^x].\n\n- item\n\n  [^x]: in list\n- other\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>item</li>
<li>other</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in list&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "only item",
			markdown: "Text [^x].\n\n- item\n\n  [^x]: in list\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>item</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in list&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition and a second paragraph stays loose",
			markdown: "Text [^x].\n\n- item\n\n  [^x]: in list\n\n  second para\n- other\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>
<p>item</p>
<p>second para</p>
</li>
<li>
<p>other</p>
</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in list&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition without a blank line",
			markdown: "Text [^x].\n\n- item\n  [^x]: in list\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>item</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in list&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition and a paragraph without blank lines",
			markdown: "Text [^x].\n\n- item\n  [^x]: in list\n\n  second para\n- other\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>
<p>item</p>
<p>second para</p>
</li>
<li>
<p>other</p>
</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in list&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definitions in several items",
			markdown: "Text [^x] [^y].\n\n- a\n  [^x]: X\n- b\n  [^y]: Y\n- c\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link> <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>a</li>
<li>b</li>
<li>c</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>X&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>Y&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "two definitions in one item",
			markdown: "Text [^x] [^y].\n\n- a\n\n  [^x]: X\n\n  [^y]: Y\n- b\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link> <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>a</li>
<li>b</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>X&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>Y&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "blank line before the next item stays loose",
			markdown: "Text [^x].\n\n- a\n\n  [^x]: X\n\n- b\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>
<p>a</p>
</li>
<li>
<p>b</p>
</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>X&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "already loose list",
			markdown: "Text [^x].\n\n- a\n\n- b\n  [^x]: in loose\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>
<p>a</p>
</li>
<li>
<p>b</p>
</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in loose&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "nested list",
			markdown: "Text [^x].\n\n- outer\n  - inner\n\n    [^x]: in inner\n  - inner2\n- outer2\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>outer
<ul>
<li>inner</li>
<li>inner2</li>
</ul>
</li>
<li>outer2</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in inner&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "nested list without a blank line",
			markdown: "Text [^x].\n\n- outer\n  - inner\n    [^x]: in inner\n  - inner2\n- outer2\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>outer
<ul>
<li>inner</li>
<li>inner2</li>
</ul>
</li>
<li>outer2</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in inner&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "outer item of a nested list",
			markdown: "Text [^x].\n\n- outer\n\n  [^x]: in outer\n- outer2\n  - inner\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>outer</li>
<li>outer2
<ul>
<li>inner</li>
</ul>
</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in outer&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "outer item without a blank line",
			markdown: "Text [^x].\n\n- outer\n  [^x]: in outer\n  - inner\n- outer2\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>outer
<ul>
<li>inner</li>
</ul>
</li>
<li>outer2</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in outer&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "ordered list",
			markdown: "Text [^x].\n\n1. item\n\n   [^x]: in ordered\n2. second\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ol>
<li>item</li>
<li>second</li>
</ol>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in ordered&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "ordered list without a blank line",
			markdown: "Text [^x].\n\n1. item\n   [^x]: in ordered\n2. second\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ol>
<li>item</li>
<li>second</li>
</ol>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>in ordered&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition opens the item",
			markdown: "Text [^x].\n\n- [^x]: def first\n- item two\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li></li>
<li>item two</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>def first&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "definition in a blockquote in an item",
			markdown: "Text [^x].\n\n- a\n  > quote\n  >\n  > [^x]: X\n- b\n",
			want: `<p>Text <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link>.</p>
<ul>
<li>a
<blockquote>
<p>quote</p>
</blockquote>
</li>
<li>b</li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>X&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
		{
			name:     "citations around a definition in a list",
			markdown: "- a [^y]\n\n  [^x]: X\n- b [^x]\n\n[^y]: Y\n",
			want: `<ul>
<li>a <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-1</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-1"><ac:link-body><sup>[1]</sup></ac:link-body></ac:link></li>
<li>b <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-ref-2</ac:parameter></ac:structured-macro><ac:link ac:anchor="footnote-2"><ac:link-body><sup>[2]</sup></ac:link-body></ac:link></li>
</ul>
<hr />
<ol>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-1</ac:parameter></ac:structured-macro>
<p>Y&#160;<ac:link ac:anchor="footnote-ref-1"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
<li><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">footnote-2</ac:parameter></ac:structured-macro>
<p>X&#160;<ac:link ac:anchor="footnote-ref-2"><ac:link-body>&#x21a9;&#xfe0e;</ac:link-body></ac:link></p>
</li>
</ol>
`,
		},
	})
}
