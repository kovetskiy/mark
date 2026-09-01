package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// extension.GFM bundles a default-configured Table, so registering it next to
// the configured NewTable put two cell renderers at goldmark's priority 500.
// PrioritizedSlice.Sort is sort.Slice, which is not stable, so which alignment
// method reached the page was decided by nothing at all -- it only happened to
// be the configured one. GFM's members are listed one by one now.
func TestTableCellAlignmentUsesTheConfiguredMethod(t *testing.T) {
	out := compileDoc(t, "| a | b |\n| :-: | --: |\n| one | two |\n")

	assert.Contains(t, out, `<th style="text-align:center">a</th>`)
	assert.Contains(t, out, `<td style="text-align:right">two</td>`)
	assert.NotContains(t, out, "align=", "the default alignment renderer emits an align attribute instead")
}

// The rest of GFM has to keep working now that it is registered piecewise.
func TestGFMMembersStayRegistered(t *testing.T) {
	assert.Contains(t, compileDoc(t, "~~gone~~\n"), "<del>gone</del>")
	assert.Contains(t, compileDoc(t, "- [x] done\n"), "<ac:task-status>complete</ac:task-status>")
	assert.Contains(t, compileDoc(t, "see https://example.com/x for more\n"), `href="https://example.com/x"`)
}
