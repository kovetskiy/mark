package markdown

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

// TestAdmonitionTitleHoldsABrace: a quoted title that ends the opening line,
// or the line up to its attributes, is read to its last quote, a "{" in it
// included.
func TestAdmonitionTitleHoldsABrace(t *testing.T) {
	for source, want := range map[string]string{
		"!!! note \"Config {env}\"\n    body\n":      "<p><strong>Config {env}</strong></p>\n<p>body</p>",
		"!!! note \"Config {env}\" {.x}\n    body\n": "<p><strong>Config {env}</strong></p>\n<p>body</p>",
		"!!! note \"say \"hi\"\"\n    body\n":        "<p><strong>say &#34;hi&#34;</strong></p>\n<p>body</p>",
	} {
		out := compileDoc(t, source, "mkdocsadmonitions")

		assert.Contains(t, out, want, "%q", source)
		assert.True(t, wellFormed(t, out), out)
	}
}
