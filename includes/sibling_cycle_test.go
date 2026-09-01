package includes

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A directive that re-includes an ancestor was only caught when it was the
// *first* directive in the fragment: everything after the first was left for
// the caller to find on a later pass, and every pass started with an empty
// stack. Two siblings, one of them circular, therefore expanded each other
// forever and the document grew until the process was killed.
func TestSiblingDirectiveReachingAnAncestorIsCircular(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("b body\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"),
		[]byte("PADDING\n<!-- Include: b.md -->\n<!-- Include: a.md -->\n"), 0644))

	_, _, _, err := ProcessIncludes(dir, "", []byte("<!-- Include: a.md -->\n"), template.New("stdlib"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular include detected")
}

// Every directive at one level is expanded by a single call, so a document with
// several includes needs no second pass from the caller.
func TestSiblingDirectivesAreAllExpandedInOneCall(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "one.md"), []byte("first"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "two.md"), []byte("second"), 0644))

	_, out, recurse, err := ProcessIncludes(
		dir, "",
		[]byte("<!-- Include: one.md -->\n\n<!-- Include: two.md -->\n"),
		template.New("stdlib"),
	)

	require.NoError(t, err)
	assert.True(t, recurse)
	assert.Equal(t, "first\n\nsecond\n", string(out))
}
