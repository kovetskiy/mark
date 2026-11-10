package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17"
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

// TestPartialRemovalRefusesAPatternOnTheCommandLine: told to archive or delete
// orphans, a partial run takes --files-from paths, not a --files wildcard, and
// refuses one before connecting.
func TestPartialRemovalRefusesAPatternOnTheCommandLine(t *testing.T) {
	noDefaultConfig(t)

	_, _, err := runMark(t, "publish", "--color", "never",
		"-b", "http://127.0.0.1:1", "-u", "user", "-p", "token",
		"--track-pages", "--partial", "--on-orphan", "delete", "--files", "docs/*.md")
	require.ErrorIs(t, err, mark.ErrPartialPattern)
	for _, name := range []string{"--on-orphan", "--partial", "--files", "--files-from"} {
		assert.ErrorContains(t, err, name)
	}
}
