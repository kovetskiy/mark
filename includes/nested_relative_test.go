package includes

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFiles lays out files under root, creating the directories they need.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
}

// A fragment's includes are written from where the fragment lives, as its links
// are. They were resolved against the document's directory instead, so a
// fragment including the file beside it failed with "no such file".
func TestNestedIncludeResolvesBesideTheFragment(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"sub/a.md": "A[<!-- Include: b.md -->]",
		"sub/b.md": "beside the fragment",
	})

	_, output, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: sub/a.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, "A[beside the fragment]", string(output))
}

// What worked before still works: a fragment naming a file from the document's
// directory finds it when there is nothing of that name beside the fragment.
func TestNestedIncludeFallsBackToTheDocumentsDirectory(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"sub/a.md":  "A[<!-- Include: shared.md -->]",
		"shared.md": "beside the document",
	})

	_, output, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: sub/a.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, "A[beside the document]", string(output))
}

// And --include-path is still looked in after both.
func TestNestedIncludeFallsBackToTheIncludePath(t *testing.T) {
	project := t.TempDir()
	shared := t.TempDir()
	writeFiles(t, project, map[string]string{"sub/a.md": "A[<!-- Include: common.md -->]"})
	writeFiles(t, shared, map[string]string{"common.md": "from the include path"})

	_, output, _, err := ProcessIncludes(
		project, shared, []byte("<!-- Include: sub/a.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, "A[from the include path]", string(output))
}

// When both exist the fragment's own wins: that is what the fragment reads as
// on its own, and what its links already resolve against.
func TestNestedIncludePrefersTheFragmentsDirectory(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"sub/a.md": "A[<!-- Include: b.md -->]",
		"sub/b.md": "beside the fragment",
		"b.md":     "beside the document",
	})

	_, output, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: sub/a.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, "A[beside the fragment]", string(output))
}

// Resolving from the fragment does not move the boundary: a fragment climbing
// out of the project is refused just as the document itself would be.
func TestNestedIncludeOutsideTheProjectIsRefused(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"secret.txt": "a private thing"})

	project := t.TempDir()
	document := filepath.Join(project, "docs")

	// Three levels up from docs/sub is the directory both temp dirs are in.
	reference := filepath.ToSlash(filepath.Join("..", "..", "..", filepath.Base(outside), "secret.txt"))
	writeFiles(t, document, map[string]string{"sub/a.md": "<!-- Include: " + reference + " -->"})

	t.Chdir(project)

	_, output, _, err := ProcessIncludes(
		document, "", []byte("<!-- Include: sub/a.md -->"), template.New("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.NotContains(t, string(output), "a private thing")
}

// Cycles are found by the file, not by the name written. Two fragments may each
// have a "b.md" of their own without that being circular...
func TestNestedIncludeOfTheSameNameIsNotACycle(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"b.md":     "top[<!-- Include: sub/a.md -->]",
		"sub/a.md": "a[<!-- Include: b.md -->]",
		"sub/b.md": "inner",
	})

	_, output, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: b.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, "top[a[inner]]", string(output))
}

// ...and one file reached by two spellings is.
func TestNestedIncludeCycleThroughARelativePath(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{
		"top.md":   "<!-- Include: sub/a.md -->",
		"sub/a.md": "<!-- Include: ../top.md -->",
	})

	_, _, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: top.md -->"), template.New("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular include detected")
}
