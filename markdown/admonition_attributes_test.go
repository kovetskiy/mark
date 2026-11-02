package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAdmonitionAttributesEndTheOpeningLine: an admonition's attribute block
// is one on its opening line, in goldmark v1's grammar. Text that is not one,
// on that line or the next, is the admonition's text.
func TestAdmonitionAttributesEndTheOpeningLine(t *testing.T) {
	for source, want := range map[string]string{
		"!!! note \"T\"\n    {x}\n":      "<p>{x}</p>",
		"!!! note \"T\"\n    {x} body\n": "<p>{x} body</p>",
		"!!! note \"T\"\n    {#x}\n":     "<p>{#x}</p>",
		"!!! note \"T\" {x}\n    body\n": "<p>{x}\nbody</p>",
	} {
		assert.Contains(t, compileDoc(t, source, "mkdocsadmonitions"), want, "%q", source)
	}

	out := compileDoc(t, "!!! note \"T\" {.c data-k=\"v\"}\n    body\n", "mkdocsadmonitions")
	assert.NotContains(t, out, "data-k", "the block is taken, not published as text")
	assert.Contains(t, out, "<p>body</p>")
}
