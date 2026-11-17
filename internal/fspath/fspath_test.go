package fspath

import (
	"os"
	"path/filepath"
	"runtime"
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

// TestResolveDeepestTakesDotDotFromTheLink: a ".." after a link climbs from
// the link's target, as the open does, rather than cancelling the link's own
// name, and still does when what follows is not there.
func TestResolveDeepestTakesDotDotFromTheLink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	if err := os.Symlink(filepath.Join(root, "a", "b"), filepath.Join(root, "docs", "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	link := root + "/docs/link/../x"
	assert.Equal(t, filepath.Join(root, "a", "x"), ResolveDeepest(link))
	assert.Equal(t, filepath.Join(root, "a", "missing", "x"), ResolveDeepest(root+"/docs/link/../missing/x"))
	assert.Equal(t, filepath.Join(root, "docs", "x"), ResolveDeepest(filepath.Join(root, "docs", "x")))
	assert.Equal(t, root, ResolveDeepest(root+"/"))
}

// TestSplitLast: the parent keeps a ".." as written.
func TestSplitLast(t *testing.T) {
	for _, c := range []struct{ path, parent, last string }{
		{"/a/b/../c", "/a/b/..", "c"},
		{"/a/b/..", "/a/b", ".."},
		{"/a", "/", "a"},
		{"/", "/", ""},
		{"a", ".", "a"},
		{".", ".", ""},
		{"a/b/", "a", "b"},
	} {
		parent, last := splitLast(filepath.FromSlash(c.path))
		assert.Equal(t, filepath.FromSlash(c.parent), parent, c.path)
		assert.Equal(t, c.last, last, c.path)
	}
}

// TestSplitLastDoubleSlash: "//a" is a root and an element on POSIX, but on
// Windows it starts a UNC path, whose volume is "\\host\share".
func TestSplitLastDoubleSlash(t *testing.T) {
	cases := []struct{ path, parent, last string }{{"//a", "/", "a"}}
	if runtime.GOOS == "windows" {
		cases = []struct{ path, parent, last string }{
			{"//host/share/a", `\\host\share\`, "a"},
			{"//host/share/a/b", `\\host\share\a`, "b"},
			{"//host/share", `\\host\share`, ""},
		}
	}

	for _, c := range cases {
		parent, last := splitLast(filepath.FromSlash(c.path))
		assert.Equal(t, filepath.FromSlash(c.parent), parent, c.path)
		assert.Equal(t, c.last, last, c.path)
	}
}
