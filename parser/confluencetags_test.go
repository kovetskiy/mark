package parser_test

import (
	"bytes"
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
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
			gm := goldmark.New(
				goldmark.WithParserOptions(
					parser.WithInlineParsers(
						util.Prioritized(cparser.NewConfluenceTagParser(), 199),
					),
				),
				goldmark.WithRendererOptions(html.WithUnsafe()),
			)

			var buf bytes.Buffer
			require.NoError(t, gm.Convert([]byte(tt.input+"\n"), &buf))
			got := buf.String()

			assert.NotContains(t, got, "&lt;/", "a closing Confluence tag must not be escaped")
			assert.Contains(t, got, tt.input, "the markup must pass through unchanged")
		})
	}
}
