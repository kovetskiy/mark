package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLayoutDirectiveInsideAContainerIsLeftAlone covers a directive written
// somewhere its pair cannot follow it.
//
// A layout directive opens an element that a later directive closes, and both
// have to end up side by side. Inside a list item, a blockquote or a table
// cell, whatever contains it closes first: the page stops being well-formed
// XML, which Confluence rejects in its entirety, and mark refuses its own
// output with an error naming XML and never the directive -- leaving the author
// nothing to go on.
func TestLayoutDirectiveInsideAContainerIsLeftAlone(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	for name, document := range map[string]string{
		"list item":  "- <!-- ac:layout-cell -->\n- second\n",
		"blockquote": "> <!-- ac:layout-cell -->\n",
		"table cell": "| a |\n| --- |\n| <!-- ac:layout-cell --> |\n",
		"paragraph":  "text <!-- ac:layout-cell --> more text\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := CompileMarkdown([]byte(document), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.NotContains(t, out, "<ac:layout-cell>",
				"a directive whose pair cannot follow it must not open an element")
			require.NoError(t, CheckWellFormed(out),
				"and whatever is published has to be something Confluence accepts")
		})
	}
}

// TestLayoutDirectiveAtDocumentLevelStillConverts is the boundary: the whole
// point of the directives is the layout they build, and that is unaffected.
func TestLayoutDirectiveAtDocumentLevelStillConverts(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	document := "<!-- ac:layout -->\n" +
		"<!-- ac:layout-section type:two_equal -->\n" +
		"<!-- ac:layout-cell -->\nleft\n<!-- ac:layout-cell end -->\n" +
		"<!-- ac:layout-cell -->\nright\n<!-- ac:layout-cell end -->\n" +
		"<!-- ac:layout-section end -->\n" +
		"<!-- ac:layout end -->\n"

	out, _, err := CompileMarkdown([]byte(document), std, "test.md", types.MarkConfig{})
	require.NoError(t, err)

	assert.Contains(t, out, "<ac:layout>")
	assert.Contains(t, out, "<ac:layout-section")
	assert.Contains(t, out, "<ac:layout-cell>")
	require.NoError(t, CheckWellFormed(out))
}
