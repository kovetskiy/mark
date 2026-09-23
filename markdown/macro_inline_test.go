package mark

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An inline macro escapes through the stdlib, takes matched text as text, and
// compiling it leaves the caller's buffer as it was -- on both compile paths.
func TestInlineMacroOnBothCompilePaths(t *testing.T) {
	const doc = "<!-- Macro: SAY\\((.*?)\\)\n" +
		"     Template: #inline\n" +
		"     inline: \"<ac:structured-macro ac:name=\\\"info\\\"><ac:rich-text-body>" +
		"<p>{{ \\\"${1}\\\" | xmlesc }}</p></ac:rich-text-body></ac:structured-macro>\" -->\n\n" +
		"SAY(helm uses {{ .Values.x }} & more)\n"

	for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
		"default": CompileMarkdown,
		"legacy":  CompileMarkdownLegacy,
	} {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			input := []byte(doc)

			out, _, err := compile(input, std, filepath.Join(t.TempDir(), "doc.md"), types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, "<p>helm uses {{ .Values.x }} &amp; more</p>")
			assert.Equal(t, doc, string(input), "the caller's buffer must not change")
		})
	}
}
