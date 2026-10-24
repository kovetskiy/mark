package mark

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// '???' becomes an expand around the panel and '???+' the panel alone, on both
// compile paths.
func TestCollapsedAdmonitionOnBothCompilePaths(t *testing.T) {
	const doc = "??? tip \"Fold & hide\"\n    Hidden.\n???\n\n???+ note \"Shown\"\n    Visible.\n"

	for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
		"default": CompileMarkdown,
		"legacy":  CompileMarkdownLegacy,
	} {
		t.Run(name, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile([]byte(doc), std, filepath.Join(t.TempDir(), "doc.md"), types.MarkConfig{
				Features: []string{"mkdocsadmonitions"},
			})
			require.NoError(t, err)

			assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Fold &amp; hide</ac:parameter>`)
			assert.Contains(t, out, `<ac:structured-macro ac:name="tip">`)
			assert.Contains(t, out, `<ac:structured-macro ac:name="note">`)
			assert.Equal(t, 1, strings.Count(out, `ac:name="expand"`))
		})
	}
}
