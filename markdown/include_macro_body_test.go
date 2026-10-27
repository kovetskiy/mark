package mark

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A macro defined in an included file reads its own arguments. The include's
// template pass used to run over the whole file, definitions included, so
// {{ .Name }} in a macro body was evaluated against the include's data (none)
// and published as <no value> before the macro was ever extracted.
func TestMacroDefinedInIncludeKeepsItsTemplateActions(t *testing.T) {
	const defs = "<!-- Macro: <!-- box:(\\w+) -->\n" +
		"     Template: #inline\n" +
		"     Name: ${1}\n" +
		"     Title: Title of ${1}\n" +
		"     inline: \"<ac:structured-macro ac:name=\\\"info\\\"><ac:parameter ac:name=\\\"title\\\">{{ .Title | xmlesc }}</ac:parameter>" +
		"<ac:rich-text-body><p>{{ .Name | xmlesc }}</p></ac:rich-text-body></ac:structured-macro>\" -->\n\n"
	const use = "# Doc\n\n<!-- box:alpha -->\n"

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"box.md": defs})

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	direct, _, err := CompileMarkdown([]byte(defs+use), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)

	std, err = stdlib.New(nil)
	require.NoError(t, err)

	included, _, err := CompileMarkdown([]byte("<!-- Include: box.md -->\n"+use), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)

	assert.NotContains(t, included, "no value")
	assert.Contains(t, included, "<p>alpha</p>")
	assert.Contains(t, included, "Title of alpha")
	assert.Equal(t, direct, included)
}
