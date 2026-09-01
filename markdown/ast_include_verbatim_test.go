package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A macro or include exists to carry storage format that goldmark would
// otherwise mangle. The sub-document's nodes were turned into raw ast.String
// nodes, and a raw string in goldmark still goes through Writer.RawWrite, which
// escapes & < > and " -- so the <ac:structured-macro> the fragment was written
// to deliver was published as visible literal text.
func TestASTIncludeKeepsConfluenceMarkupVerbatim(t *testing.T) {
	dir, doc := writeMacroIncludingFragment(t,
		`<ac:structured-macro ac:name="info"><ac:rich-text-body>from a fragment</ac:rich-text-body></ac:structured-macro>`)

	out := compileInDir(t, dir, doc)

	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
	assert.NotContains(t, out, "&lt;ac:structured-macro", "storage format must not be escaped into literal text")
}
