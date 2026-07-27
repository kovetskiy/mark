package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detailsMacroDoc wraps body in an ac:details macro. The pattern is a plain
// marker rather than the conventional nested-comment form, so this exercises the
// template's block separation on its own without also depending on how a nested
// directive's terminator is located.
func detailsMacroDoc(body string) []byte {
	return []byte(`<!-- Macro: :dtl:
     Template: ac:details
     Body: "` + body + `"
-->

:dtl:

After.
`)
}

// The ac:details template joined its lines with no separator, so
// </ac:rich-text-body> landed on the line straight after the body. A body ending
// in a table or list therefore absorbed the closing tags as further content, and
// a bare paragraph was never wrapped in <p> at all.
func TestDetailsMacroClosesOutsideBody(t *testing.T) {
	tests := []struct {
		name string
		// body is interpolated into a quoted macro config value, so newlines are
		// written as the two-character escape the config parser unescapes.
		body string
		// wantContains is markup that must appear intact in the output.
		wantContains []string
	}{
		{
			name: "table body",
			body: `| A | B |\n|---|---|\n| 1 | 2 |`,
			wantContains: []string{
				"<td>1</td>",
				"</tbody>\n</table>\n</ac:rich-text-body></ac:structured-macro>",
			},
		},
		{
			name: "list body",
			body: `- one\n- two`,
			wantContains: []string{
				"<li>two</li>",
				"</ul>\n</ac:rich-text-body></ac:structured-macro>",
			},
		},
		{
			name: "paragraph body",
			body: "Just a paragraph.",
			wantContains: []string{
				"<p>Just a paragraph.</p>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			htmlOutput, _, err := CompileMarkdown(detailsMacroDoc(tt.body), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, htmlOutput, `ac:name="details"`, "the macro must expand")
			assert.Contains(t, htmlOutput, "<p>After.</p>", "text after the macro must survive")
			assert.NotContains(t, htmlOutput, "Macro:", "the directive must be stripped")

			for _, want := range tt.wantContains {
				assert.Contains(t, htmlOutput, want)
			}

			// The closing tags must never end up inside a table cell or list item.
			assert.NotContains(t, htmlOutput, "<td></ac:rich-text-body>")
			assert.NotContains(t, htmlOutput, "</ac:rich-text-body></ac:structured-macro></li>")
		})
	}
}
