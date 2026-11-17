package attachment

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// caseFoldingDir is a fresh directory on a file system that ignores the
// case of names, as macOS and Windows do by default, and skips the test
// where the temporary directory is on one that does not. Point TMPDIR at
// such a file system to run it elsewhere.
func caseFoldingDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "probe"), 0o755))
	if _, err := os.Stat(filepath.Join(dir, "PROBE")); errors.Is(err, os.ErrNotExist) {
		t.Skip("the temporary directory is on a file system that tells case apart")
	}

	require.NoError(t, os.Remove(filepath.Join(dir, "probe")))

	return dir
}

// TestBoundaryIgnoresCaseWhereTheFileSystemDoes: where "Docs" and "docs" are
// one directory, a root, a document's directory or a working directory
// spelt in another case than the file still holds it, and a file outside is
// still refused.
func TestBoundaryIgnoresCaseWhereTheFileSystemDoes(t *testing.T) {
	repo := caseFoldingDir(t)

	for _, name := range []string{"docs/a.txt", "other/b.txt"} {
		name = filepath.Join(repo, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, os.WriteFile(name, []byte(name), 0o600))
	}

	t.Chdir(repo)

	root := Root{}.WithBoundary("Docs")
	require.NoError(t, Boundary("Docs").Validate())
	require.NoError(t, root.checkAttachment("docs", "a.txt"))
	require.NoError(t, root.CheckReadable("docs", filepath.Join(repo, "docs", "a.txt")))
	require.ErrorIs(t, root.checkAttachment("docs", "../other/b.txt"), ErrOutsideProject)

	document := Root{}.WithBoundary(BoundaryDocument)
	require.NoError(t, document.checkAttachment("Docs", "../docs/a.txt"))
	require.NoError(t, document.CheckReadable("DOCS", filepath.Join(repo, "docs", "a.txt")))
	require.ErrorIs(t, document.checkAttachment("Docs", "../other/b.txt"), ErrOutsideProject)

	// The working directory spelt in another case holds the file, which
	// the document's directory does not.
	t.Chdir(filepath.Join(repo, "DOCS"))
	require.NoError(t, Root{}.checkAttachment("../other", "../docs/a.txt"))
	require.NoError(t, CheckReadable("../other", filepath.Join(repo, "docs", "a.txt")))

	t.Chdir(repo)

	inFS := FSRoot(os.DirFS(repo)).WithBoundary(BoundaryDocument)
	require.NoError(t, inFS.checkAttachment("Docs", "../docs/a.txt"))
	require.NoError(t, inFS.CheckReadable("Docs", "../docs/a.txt"))
	require.ErrorIs(t, inFS.checkAttachment("Docs", "../other/b.txt"), ErrOutsideProject)
}
