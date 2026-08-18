package mark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileWithTemplate renders a document against a template of the given name
// placed on the include path.
func compileWithTemplate(t *testing.T, name, template, document string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(template), 0o600))

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown([]byte(document), lib, "testdata/x.md",
		types.MarkConfig{MermaidScale: 1, D2Scale: 1, IncludePath: dir})
	require.NoError(t, err)

	return out
}

const drawioDocument = "<!-- Macro: %%drawio%%\n" +
	"     Template: inc-drawio\n" +
	"     Name: diagram.drawio -->\n\n%%drawio%%\n"

// TestCustomTemplateOutputIsPreservedVerbatim pins mark's side of issue #908: a
// template's output is injected exactly as written, whitespace included.
//
// That is the contract, and it is also the trap. A storage-format element
// surrounded by newlines inside <ac:parameter> makes the parameter mixed
// content, and Confluence resolves that by writing the element out as a string
// -- which is where AttachmentResourceIdentifier[...] comes from. It is worth a
// test because the mangling happens on the server, so nothing here would ever
// show it.
func TestCustomTemplateOutputIsPreservedVerbatim(t *testing.T) {
	out := compileWithTemplate(t, "inc-drawio",
		"<ac:structured-macro ac:name=\"inc-drawio\">\n"+
			"  <ac:parameter ac:name=\"name\">\n"+
			"    <ri:attachment ri:filename=\"{{ .Name }}\" />\n"+
			"  </ac:parameter>\n"+
			"</ac:structured-macro>\n",
		drawioDocument)

	assert.Contains(t, out, `<ri:attachment ri:filename="diagram.drawio" />`,
		"mark must not alter what a template produced")
	assert.Regexp(t, `(?s)<ac:parameter ac:name="name">\s*\n`, out,
		"the newlines the template asked for are still there")
}

// TestCustomTemplateCanEmitWhitespaceFreeParameters is the way out. Go's trim
// markers let a template stay readable while emitting nothing between the
// parameter and the element inside it, which is what the built-in templates do
// by being joined without separators.
func TestCustomTemplateCanEmitWhitespaceFreeParameters(t *testing.T) {
	out := compileWithTemplate(t, "inc-drawio",
		"<ac:structured-macro ac:name=\"inc-drawio\">\n"+
			"  {{- \"\" -}}\n"+
			"  <ac:parameter ac:name=\"name\">\n"+
			"    {{- \"\" -}}\n"+
			"    <ri:attachment ri:filename=\"{{ .Name }}\"/>\n"+
			"    {{- \"\" -}}\n"+
			"  </ac:parameter>\n"+
			"  {{- \"\" -}}\n"+
			"</ac:structured-macro>\n",
		drawioDocument)

	assert.Contains(t, out,
		`<ac:parameter ac:name="name"><ri:attachment ri:filename="diagram.drawio"/></ac:parameter>`,
		"nothing may sit between the parameter and the element inside it")
}

// TestBuiltinTemplateHasNoWhitespaceInItsParameter is the comparison the issue
// rests on. The built-in works where the custom one did not, and this is the
// whole of the difference: the built-in emits no whitespace, because its lines
// are joined with nothing between them.
func TestBuiltinTemplateHasNoWhitespaceInItsParameter(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown(
		[]byte("<!-- Include: ac:view-file\n     Name: diagram.drawio -->\n"),
		lib, "testdata/x.md", types.MarkConfig{MermaidScale: 1, D2Scale: 1})
	require.NoError(t, err)

	assert.Contains(t, out,
		`<ac:parameter ac:name="name"><ri:attachment ri:filename="diagram.drawio"/></ac:parameter>`)
	assert.NotContains(t, strings.SplitN(out, "</ac:parameter>", 2)[0], "\n",
		"the built-in parameter holds no newline, which is why it survives")
}
