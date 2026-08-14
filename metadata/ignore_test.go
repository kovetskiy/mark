package metadata

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripIgnoredBlocks(t *testing.T) {
	t.Run("removes the region and its markers", func(t *testing.T) {
		out, err := StripIgnoredBlocks([]byte("A\n<!-- ac:ignore -->\nB\nC\n<!-- ac:ignore end -->\nD\n"))
		require.NoError(t, err)
		assert.Equal(t, "A\nD\n", string(out))
	})

	t.Run("a document without markers is returned untouched", func(t *testing.T) {
		in := []byte("A\n\nB\n")
		out, err := StripIgnoredBlocks(in)
		require.NoError(t, err)
		assert.Equal(t, string(in), string(out))
	})

	t.Run("several regions", func(t *testing.T) {
		out, err := StripIgnoredBlocks([]byte(
			"A\n<!-- ac:ignore -->\nx\n<!-- ac:ignore end -->\nB\n<!-- ac:ignore -->\ny\n<!-- ac:ignore end -->\nC\n"))
		require.NoError(t, err)
		assert.Equal(t, "A\nB\nC\n", string(out))
	})

	t.Run("spacing and case are tolerated", func(t *testing.T) {
		out, err := StripIgnoredBlocks([]byte("A\n<!--AC:Ignore-->\nx\n<!--   ac:ignore   end   -->\nB\n"))
		require.NoError(t, err)
		assert.Equal(t, "A\nB\n", string(out))
	})

	// The end marker starts with the start marker's text, so a naive prefix
	// test reads every "ac:ignore end" as another opening and the region never
	// closes.
	t.Run("the end marker is not mistaken for another start", func(t *testing.T) {
		out, err := StripIgnoredBlocks([]byte("A\n<!-- ac:ignore -->\nx\n<!-- ac:ignore end -->\nB\n"))
		require.NoError(t, err)
		assert.Equal(t, "A\nB\n", string(out))
	})

	t.Run("lines are removed rather than blanked", func(t *testing.T) {
		// A blank line is not nothing in Markdown: left in the middle of a list
		// it would split it in two.
		out, err := StripIgnoredBlocks([]byte(
			"- one\n<!-- ac:ignore -->\n- github only\n<!-- ac:ignore end -->\n- two\n"))
		require.NoError(t, err)
		assert.Equal(t, "- one\n- two\n", string(out))
	})
}

func TestStripIgnoredBlocksReportsMistakes(t *testing.T) {
	// Quietly publishing half a page is a worse answer than refusing, and the
	// line number has to be the one in the file the author is looking at.
	t.Run("unclosed region", func(t *testing.T) {
		_, err := StripIgnoredBlocks([]byte("A\nB\n<!-- ac:ignore -->\nC\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "line 3")
		assert.Contains(t, err.Error(), "never closed")
	})

	t.Run("end without a start", func(t *testing.T) {
		_, err := StripIgnoredBlocks([]byte("A\n<!-- ac:ignore end -->\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "line 2")
		assert.Contains(t, err.Error(), "without a matching")
	})

	t.Run("region opened twice", func(t *testing.T) {
		_, err := StripIgnoredBlocks([]byte("<!-- ac:ignore -->\nA\n<!-- ac:ignore -->\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "line 3")
	})
}

// TestStripIgnoredBlocksDoesNotPanicOnShortComments guards the same class of
// crash as issue #686: "<!-->" satisfies both the opening and closing test at
// once, because the "-->" is found inside the "<!--".
func TestStripIgnoredBlocksDoesNotPanicOnShortComments(t *testing.T) {
	for _, line := range []string{"<!-->", "<!--", "-->", "<!---->"} {
		_, err := StripIgnoredBlocks([]byte("<!-- ac:ignore -->\n" + line + "\n<!-- ac:ignore end -->\n"))
		require.NoError(t, err, "input %q", line)
	}

	// And outside a region.
	out, err := StripIgnoredBlocks([]byte("<!-->\nA\n<!-- ac:ignore -->\nx\n<!-- ac:ignore end -->\n"))
	require.NoError(t, err)
	assert.Equal(t, "<!-->\nA\n", strings.TrimSuffix(string(out), "\n")+"\n")
}
