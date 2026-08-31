package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
)

// TestIndentedCodeBlockBecomesTheCodeMacro covers the block written by
// indentation rather than by fences. Confluence has no <pre>, so it has to
// become the code macro like any other code block.
func TestIndentedCodeBlockBecomesTheCodeMacro(t *testing.T) {
	lib := newStdlib(t)

	actual := render(t, "    package main\n    func main() {}\n", []renderer.NodeRenderer{
		crenderer.NewConfluenceCodeBlockRenderer(lib, "test.md"),
	})
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:structured-macro ac:name="code">`)
	assert.Contains(t, actual, `<ac:parameter ac:name="language"></ac:parameter>`,
		"an indented block names no language")
	assert.Contains(t, actual, `<ac:parameter ac:name="collapse">false</ac:parameter>`)
	assert.Contains(t, actual, "<![CDATA[package main\nfunc main() {}]]>",
		"the trailing newline is trimmed, so the macro does not gain a blank last line")
}

// TestIndentedCodeBlockEscapesTheCDATATerminator covers a code sample that
// contains "]]>" -- XML, a shell heredoc, anything -- which would end the CDATA
// section in the middle of the sample and leave the rest as markup.
func TestIndentedCodeBlockEscapesTheCDATATerminator(t *testing.T) {
	lib := newStdlib(t)

	actual := render(t, "    <![CDATA[x]]>\n", []renderer.NodeRenderer{
		crenderer.NewConfluenceCodeBlockRenderer(lib, "test.md"),
	})
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `]]><![CDATA[]]]]><![CDATA[>`)
}
