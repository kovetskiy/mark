package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ac:title was escaped but ri:filename was not, so a quote in a diagram title
// closed the attribute early and produced malformed XML. convertAttachment
// output always lands in an attribute, so it escapes as well as flattening
// slashes.
func TestAttachmentFilenameAttributeIsEscaped(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"quote", `My "quoted" Diagram`, `ri:filename="My &#34;quoted&#34; Diagram.png"`},
		{"ampersand and angles", `A & B <c>`, `ri:filename="A &amp; B &lt;c&gt;.png"`},
		{"plain title unchanged", `My Diagram`, `ri:filename="My Diagram.png"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown(
				[]byte("```d2 title "+tt.title+"\na -> b\n```\n"),
				std, "test.md", types.MarkConfig{Features: []string{"d2"}, D2Scale: 1.0},
			)
			require.NoError(t, err)

			assert.Contains(t, out, tt.want)
		})
	}
}
