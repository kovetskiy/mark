package parser_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v17/internal/goldmarktest"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// The tag parser routes Confluence tags to RawHTML so they are not escaped. Its
// close-tag pattern covered only "ac:", so a separate "</ri:...>" closing tag
// fell through to the text renderer and was entity-escaped, breaking the element
// it was meant to close. The open-tag pattern already covered both prefixes.
func TestConfluenceTagParserClosingTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "ri:page with a separate closing tag",
			input: `<ac:link><ri:page ri:content-title="X"></ri:page></ac:link>`,
		},
		{
			name:  "ri:url with a separate closing tag",
			input: `<ac:image><ri:url ri:value="https://x/a.png"></ri:url></ac:image>`,
		},
		{
			name:  "ac: closing tag keeps working",
			input: `<ac:structured-macro ac:name="info"><ac:rich-text-body>x</ac:rich-text-body></ac:structured-macro>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := goldmarktest.New(
				goldmarktest.WithParserOptions(parser.WithInlineParsers(
					util.Prioritized(cparser.NewConfluenceTagParser(), 199),
				)),
				goldmarktest.WithRendererOptions(html.WithUnsafe()),
			)

			var buf bytes.Buffer
			require.NoError(t, md.Convert([]byte(tt.input+"\n"), &buf))
			got := buf.String()

			assert.NotContains(t, got, "&lt;/", "a closing Confluence tag must not be escaped")
			assert.Contains(t, got, tt.input, "the markup must pass through unchanged")
		})
	}
}

func convertWithTagParser(t *testing.T, input string) string {
	t.Helper()

	md := goldmarktest.New(
		goldmarktest.WithParserOptions(parser.WithInlineParsers(
			util.Prioritized(cparser.NewConfluenceTagParser(), 199),
		)),
		goldmarktest.WithRendererOptions(html.WithUnsafe()),
	)

	var buf bytes.Buffer
	require.NoError(t, md.Convert([]byte(input), &buf))

	return buf.String()
}

// The text of a parameter, and of the other elements that hold a string, is
// published as written; the text around them is still Markdown.
func TestConfluenceTagParserLiteralElements(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "a parameter",
			input: `*a* <ac:parameter ac:name="v">.*b.* _c_</ac:parameter> *d*`,
			want:  `<em>a</em> <ac:parameter ac:name="v">.*b.* _c_</ac:parameter> <em>d</em>`,
		},
		{
			name:  "a default parameter",
			input: "<ac:default-parameter>`a`</ac:default-parameter>",
			want:  "<ac:default-parameter>`a`</ac:default-parameter>",
		},
		{
			name:  "a task id and status",
			input: `<ac:task-id>_1_</ac:task-id><ac:task-status>*complete*</ac:task-status>`,
			want:  `<ac:task-id>_1_</ac:task-id><ac:task-status>*complete*</ac:task-status>`,
		},
		{
			name:  "one parameter after another",
			input: `<ac:parameter ac:name="a">*1</ac:parameter><ac:parameter ac:name="b">2*</ac:parameter>`,
			want:  `<ac:parameter ac:name="a">*1</ac:parameter><ac:parameter ac:name="b">2*</ac:parameter>`,
		},
		{
			name:  "text that has to be escaped",
			input: `<ac:parameter ac:name="v">a < b && "c" > d &amp; &#42;e&#42; <b>f</b></ac:parameter>`,
			want:  `<ac:parameter ac:name="v">a &lt; b &amp;&amp; &quot;c&quot; &gt; d &amp; *e* &lt;b&gt;f&lt;/b&gt;</ac:parameter>`,
		},
		{
			name:  "an element beside text",
			input: `<ac:parameter ac:name="v">*a* <ri:url ri:value="b*c*d"/> *e*</ac:parameter>`,
			want:  `<ac:parameter ac:name="v">*a* <ri:url ri:value="b*c*d"/> *e*</ac:parameter>`,
		},
		{
			name:  "a comment",
			input: `<ac:parameter ac:name="v">a<!-- </ac:parameter> *b* -->*c*</ac:parameter>`,
			want:  `<ac:parameter ac:name="v">a<!-- </ac:parameter> *b* -->*c*</ac:parameter>`,
		},
		{
			name:  "a tag and a value over two lines",
			input: "<ac:parameter\n ac:name=\"v\">*a*\n*b*</ac:parameter> *c*",
			want:  "<ac:parameter\nac:name=\"v\">*a*\n*b*</ac:parameter> <em>c</em>",
		},
		{
			name:  "a rich text body is Markdown",
			input: `<ac:rich-text-body>*a*</ac:rich-text-body>`,
			want:  `<ac:rich-text-body><em>a</em></ac:rich-text-body>`,
		},
		{
			name:  "an element of another name",
			input: `<ac:parameters>*a*</ac:parameters>`,
			want:  `<ac:parameters><em>a</em></ac:parameters>`,
		},
		{
			name:  "a parameter closed in another paragraph",
			input: "<ac:parameter ac:name=\"v\">*a*\n\n*b*</ac:parameter>",
			want:  "<ac:parameter ac:name=\"v\"><em>a</em></p>\n<p><em>b</em></ac:parameter>",
		},
		{
			name:  "a parameter that is not closed, and one after it that is",
			input: "<ac:parameter ac:name=\"v\">*a* <ac:task-id>*1*</ac:task-id>\n\n<ac:parameter ac:name=\"w\">*b*</ac:parameter>",
			want:  "<ac:parameter ac:name=\"v\"><em>a</em> <ac:task-id>*1*</ac:task-id></p>\n<p><ac:parameter ac:name=\"w\">*b*</ac:parameter>",
		},
		{
			name:  "two parameters that are not closed",
			input: `<ac:parameter ac:name="v">*a* <ac:parameter ac:name="w">*b*`,
			want:  `<ac:parameter ac:name="v"><em>a</em> <ac:parameter ac:name="w"><em>b</em>`,
		},
		{
			name:  "a parameter ends at its first closing tag",
			input: `<ac:parameter ac:name="v">*a*<ac:parameter ac:name="w">*b*</ac:parameter> *c*</ac:parameter>`,
			want:  `<ac:parameter ac:name="v">*a*<ac:parameter ac:name="w">*b*</ac:parameter> <em>c</em></ac:parameter>`,
		},
		{
			name:  "a section over two lines",
			input: "<ac:parameter ac:name=\"v\"><![CDATA[a </ac:parameter> *b*\nc]]>*d*</ac:parameter> *e*",
			want:  "<ac:parameter ac:name=\"v\"><![CDATA[a </ac:parameter> *b*\nc]]>*d*</ac:parameter> <em>e</em>",
		},
		{
			name:  "a comment over two lines",
			input: "<ac:parameter ac:name=\"v\"><!-- a </ac:parameter>\nb -->*c*</ac:parameter>",
			want:  "<ac:parameter ac:name=\"v\"><!-- a </ac:parameter>\nb -->*c*</ac:parameter>",
		},
		{
			name:  "a section that is not closed, before a second line",
			input: "<ac:parameter ac:name=\"v\">*a* <![CDATA[ b</ac:parameter>\n*c*",
			want:  "<ac:parameter ac:name=\"v\">*a* &lt;![CDATA[ b</ac:parameter>\n<em>c</em>",
		},
		{
			name:  "a section that is not closed is text",
			input: `<ac:parameter ac:name="v">*a* <![CDATA[ b</ac:parameter>`,
			want:  `<ac:parameter ac:name="v">*a* &lt;![CDATA[ b</ac:parameter>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, "<p>"+tt.want+"</p>\n", convertWithTagParser(t, tt.input+"\n"))
		})
	}
}

// Each tag stays a node of its own, which is what the transformers and the
// paragraph renderer read to tell where an element opens and closes.
func TestConfluenceTagParserLiteralElementNodes(t *testing.T) {
	source := []byte(`<ac:parameter ac:name="v">a*b <ri:url ri:value="c"/></ac:parameter>` + "\n")

	doc := parser.New(parser.WithInlineParsers(
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	)).Parse(source)

	var got []string
	for child := doc.FirstChild().FirstChild(); child != nil; child = child.NextSibling() {
		raw, ok := child.(*ast.RawHTML)
		require.True(t, ok, child.Kind().String())
		got = append(got, string(raw.Value.Bytes(source)))
	}

	assert.Equal(t, []string{
		`<ac:parameter ac:name="v">`,
		`a*b `,
		`<ri:url ri:value="c"/>`,
		`</ac:parameter>`,
	}, got)
}

// A block of opening tags that are never closed is read for a closing tag
// once, not once from each of them.
func TestConfluenceTagParserUnclosedLiteralElements(t *testing.T) {
	const tags = 4000

	input := strings.Repeat(`<ac:parameter ac:name="v">*a* `, tags) + "\n"

	start := time.Now()
	got := convertWithTagParser(t, input)

	assert.Equal(t, tags, strings.Count(got, `<ac:parameter ac:name="v"><em>a</em>`))
	assert.Less(t, time.Since(start), 30*time.Second)
}
