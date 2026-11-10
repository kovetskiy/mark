package fspath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnderAPathThatDoesNotExist: symlinks are resolved as deep as a path
// exists, so a directory not made yet under a symlinked root is still inside
// it.
func TestUnderAPathThatDoesNotExist(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	rel, ok := Under(root, filepath.Join(link, "not", "yet"))
	require.True(t, ok)
	assert.Equal(t, filepath.Join("not", "yet"), rel)

	_, ok = Under(root, filepath.Join(t.TempDir(), "elsewhere"))
	assert.False(t, ok)
}

// TestWithinComparesAsGiven: "../x" is not under its root however it is
// spelled, and a root contains itself.
func TestWithinComparesAsGiven(t *testing.T) {
	root := t.TempDir()

	rel, ok := Within(root, root)
	require.True(t, ok)
	assert.Equal(t, ".", rel)

	_, ok = Within(root, filepath.Join(root, "..", "x"))
	assert.False(t, ok)

	rel, ok = Within(root, filepath.Join(root, "..x"))
	require.True(t, ok)
	assert.Equal(t, "..x", rel)
}
