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

// TestParameterHoldingAnElementIsTightened is issue #908, written the way the
// reporter wrote it: across lines, indented, readable.
//
// The newlines would otherwise be published inside the parameter, leaving it
// holding text as well as an element, which Confluence resolves by writing the
// element out as a string.
func TestParameterHoldingAnElementIsTightened(t *testing.T) {
	out := compileWithTemplate(t, "inc-drawio",
		"<ac:structured-macro ac:name=\"inc-drawio\">\n"+
			"  <ac:parameter ac:name=\"name\">\n"+
			"    <ri:attachment ri:filename=\"{{ .Name }}\" />\n"+
			"  </ac:parameter>\n"+
			"</ac:structured-macro>\n",
		drawioDocument)

	assert.Contains(t, out,
		`<ac:parameter ac:name="name"><ri:attachment ri:filename="diagram.drawio" /></ac:parameter>`,
		"nothing may sit between the parameter and the element inside it")
}

// TestParameterHoldingAStringIsUntouched: there the spacing is the value, and
// mark has no business deciding what somebody meant by it.
func TestParameterHoldingAStringIsUntouched(t *testing.T) {
	out := compileWithTemplate(t, "inc-title",
		"<ac:structured-macro ac:name=\"inc-title\">"+
			"<ac:parameter ac:name=\"title\">  {{ .Name }}  </ac:parameter>"+
			"</ac:structured-macro>\n",
		"<!-- Macro: %%t%%\n     Template: inc-title\n     Name: spaced -->\n\n%%t%%\n")

	assert.Contains(t, out, `<ac:parameter ac:name="title">  spaced  </ac:parameter>`,
		"a string parameter keeps the spacing it was given")
}

// TestBodyWhitespaceIsPreserved is the property that rules out doing this more
// widely. The blank lines inside a rich text body are what let a macro's body
// be read as Markdown blocks; a body ending in a list would otherwise swallow
// the closing tags.
func TestBodyWhitespaceIsPreserved(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown([]byte(
		"<!-- Macro: :m:\n     Template: ac:box\n     Name: info\n"+
			"     Body: \"- one\\n- two\" -->\n\n:m:\n"),
		lib, "testdata/x.md", types.MarkConfig{MermaidScale: 1, D2Scale: 1})
	require.NoError(t, err)

	assert.Contains(t, out, "<li>one</li>", "the body must still be read as Markdown")
	assert.Contains(t, out, "</ul>\n</ac:rich-text-body>",
		"the closing tags must still sit outside the list")
}

// TestBuiltinTemplateStillMatches: the built-in was already tight, being joined
// with nothing between its lines, and must stay that way.
func TestBuiltinTemplateStillMatches(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := mark.CompileMarkdown(
		[]byte("<!-- Include: ac:view-file\n     Name: diagram.drawio -->\n"),
		lib, "testdata/x.md", types.MarkConfig{MermaidScale: 1, D2Scale: 1})
	require.NoError(t, err)

	assert.Contains(t, out,
		`<ac:parameter ac:name="name"><ri:attachment ri:filename="diagram.drawio"/></ac:parameter>`)
	assert.NotContains(t, strings.SplitN(out, "</ac:parameter>", 2)[0], "\n")
}
