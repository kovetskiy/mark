package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAdmonitionWithoutAnIndentedBodyIsItsTitle: what follows an admonition
// with no indented body and no closing run is published after the box, not
// in it.
func TestAdmonitionWithoutAnIndentedBodyIsItsTitle(t *testing.T) {
	const box = `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>` + "\n" +
		`<p><strong>Remember</strong></p>` + "\n" +
		`</ac:rich-text-body></ac:structured-macro>` + "\n"

	for name, source := range map[string]string{
		"blank line":      "!!! note \"Remember\"\n\nNext paragraph.\n\n## Heading\n",
		"unindented line": "!!! note \"Remember\"\nNext paragraph.\n\n## Heading\n",
	} {
		t.Run(name, func(t *testing.T) {
			out := compileDoc(t, source, "mkdocsadmonitions")

			after, found := strings.CutPrefix(out, box)
			if assert.True(t, found, "the box holds its title only: %s", out) {
				assert.Contains(t, after, "<p>Next paragraph.</p>")
				assert.Contains(t, after, "Heading</h2>")
			}
		})
	}
}

// TestAdmonitionUnindentedBodyUpToAClosingRun: a closing run keeps the lines
// before it in the box, indented or not.
func TestAdmonitionUnindentedBodyUpToAClosingRun(t *testing.T) {
	out := compileDoc(t, "!!! note \"Remember\"\nIn the box.\n!!!\n\nAfter the box.\n", "mkdocsadmonitions")

	assert.Equal(t, `<ac:structured-macro ac:name="info"><ac:parameter ac:name="icon">true</ac:parameter><ac:rich-text-body>`+"\n"+
		`<p><strong>Remember</strong></p>`+"\n"+
		`<p>In the box.</p>`+"\n"+
		`</ac:rich-text-body></ac:structured-macro>`+"\n"+
		`<p>After the box.</p>`+"\n", out)
}
