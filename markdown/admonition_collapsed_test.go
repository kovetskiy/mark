package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// '???' becomes an expand around the panel and '???+' the panel alone.
func TestCollapsedAdmonition(t *testing.T) {
	const doc = "??? tip \"Fold & hide\"\n    Hidden.\n???\n\n???+ note \"Shown\"\n    Visible.\n"

	out := compileDoc(t, doc, "mkdocsadmonitions")

	assert.Contains(t, out, `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Fold &amp; hide</ac:parameter>`)
	assert.Contains(t, out, `<ac:structured-macro ac:name="tip">`)
	assert.Contains(t, out, `<ac:structured-macro ac:name="info">`)
	assert.Equal(t, 1, strings.Count(out, `ac:name="expand"`))
}
