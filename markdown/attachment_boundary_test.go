package markdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundarySources are the ways a document names a file beside another
// document: an image, an <img>, a link attached with AttachReferenced, and a
// file a macro names in its Attachment key.
var boundarySources = []string{
	"![logo](../other-doc/logo.png)\n",
	"<img src=\"../other-doc/logo.png\">\n",
	"[the file](../other-doc/file.pdf)\n",
	widthMacroForTest + "\n![A](../other-doc/logo.png)<!-- width=10 -->\n",
}

// TestCompileHoldsReferencedFilesToTheRootsBoundary: every file the document
// names is held to the Boundary Options.Root carries. Beside another
// document it passes the default boundary, which holds the working directory,
// and fails attachment.BoundaryDocument or a directory that does not hold it;
// below the document it passes both.
func TestCompileHoldsReferencedFilesToTheRootsBoundary(t *testing.T) {
	repo := t.TempDir()
	docs := filepath.Join(repo, "guide")
	other := filepath.Join(repo, "other-doc")
	require.NoError(t, os.MkdirAll(filepath.Join(docs, "images"), 0o755))
	require.NoError(t, os.Mkdir(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "file.pdf"), []byte("pdf"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(other, "logo.png"), []byte("png"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "images", "logo.png"), []byte("png"), 0o600))
	t.Chdir(repo)

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	document := filepath.Join(docs, "index.md")
	compile := func(source string, boundary attachment.Boundary) ([]attachment.Attachment, error) {
		_, attached, err := CompileMarkdown(t.Context(), []byte(source), std, document,
			Options{AttachReferenced: true, Root: attachment.Root{}.WithBoundary(boundary)})

		return attached, err
	}

	for _, source := range boundarySources {
		attached, err := compile(source, "")
		require.NoError(t, err, source)
		assert.Len(t, attached, 1, source)

		_, err = compile(source, attachment.BoundaryDocument)
		require.ErrorIs(t, err, attachment.ErrOutsideProject, source)

		_, err = compile(source, attachment.Boundary(docs))
		require.ErrorIs(t, err, attachment.ErrOutsideProject, source)

		attached, err = compile(source, attachment.Boundary(repo))
		require.NoError(t, err, source)
		assert.Len(t, attached, 1, source)
	}

	attached, err := compile("![logo](images/logo.png)\n", attachment.BoundaryDocument)
	require.NoError(t, err)
	assert.Len(t, attached, 1)
}

// TestCompileInAnFSHoldsReferencedFilesToTheRootsBoundary: a document in an
// fs.FS may name anything inside it by default, and only what is in its own
// directory under attachment.BoundaryDocument.
func TestCompileInAnFSHoldsReferencedFilesToTheRootsBoundary(t *testing.T) {
	t.Chdir(t.TempDir())

	files := fstest.MapFS{
		"guide/images/logo.png": {Data: []byte("png")},
		"other-doc/logo.png":    {Data: []byte("png")},
		"other-doc/file.pdf":    {Data: []byte("pdf")},
	}

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	compile := func(source string, boundary attachment.Boundary) ([]attachment.Attachment, error) {
		_, attached, err := CompileMarkdown(t.Context(), []byte(source), std, "guide/index.md",
			Options{AttachReferenced: true, Root: attachment.FSRoot(files).WithBoundary(boundary)})

		return attached, err
	}

	for _, source := range boundarySources {
		attached, err := compile(source, "")
		require.NoError(t, err, source)
		assert.Len(t, attached, 1, source)

		_, err = compile(source, attachment.BoundaryDocument)
		require.ErrorIs(t, err, attachment.ErrOutsideProject, source)
	}

	attached, err := compile("![logo](images/logo.png)\n", attachment.BoundaryDocument)
	require.NoError(t, err)
	assert.Len(t, attached, 1)
}

// TestCompileIncludesKeepTheirOwnBoundary: an include is a template expanded
// into the document, not a file published with it, so a fragment beside
// another document, or under IncludePath, is still included under
// attachment.BoundaryDocument, on disk and in an fs.FS alike.
func TestCompileIncludesKeepTheirOwnBoundary(t *testing.T) {
	repo := t.TempDir()
	includePath := filepath.Join(repo, "templates")
	for _, dir := range []string{"guide", "shared", "templates"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repo, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, "shared", "fragment.md"), []byte("fragment text\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(includePath, "common.md"), []byte("common text\n"), 0o600))
	t.Chdir(repo)

	files := fstest.MapFS{"shared/fragment.md": {Data: []byte("fragment text\n")}}

	for _, root := range []attachment.Root{{}, attachment.FSRoot(files)} {
		std, err := stdlib.New(context.Background(), nil)
		require.NoError(t, err)

		html, _, err := CompileMarkdown(t.Context(),
			[]byte("<!-- Include: ../shared/fragment.md -->\n\n<!-- Include: common.md -->\n"), std,
			filepath.Join("guide", "index.md"),
			Options{Root: root.WithBoundary(attachment.BoundaryDocument), IncludePath: includePath})
		require.NoError(t, err)
		assert.Contains(t, html, "fragment text")
		assert.Contains(t, html, "common text")
	}
}
