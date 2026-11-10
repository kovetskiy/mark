package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadPaths: one path a line, as git diff --name-only writes them, with
// CRLF and blank lines tolerated and spaces and glob characters kept.
func TestReadPaths(t *testing.T) {
	list := filepath.Join(t.TempDir(), "changed")
	require.NoError(t, os.WriteFile(list, []byte("docs/a.md\r\n\ndocs/with space.md\ndocs/[x]{y,z}*.md"), 0o600))

	paths, err := readPaths(list, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"docs/a.md", "docs/with space.md", "docs/[x]{y,z}*.md"}, paths)

	paths, err = readPaths("", "")
	require.NoError(t, err)
	assert.Nil(t, paths)

	_, err = readPaths("-", "-")
	require.ErrorContains(t, err, "cannot both read standard input")
}
