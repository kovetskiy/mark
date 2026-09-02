package attachment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// project builds a repository with a document directory inside it, and returns
// the two paths. The test runs from the repository, as a publish does.
func project(t *testing.T) (root, docs string) {
	t.Helper()

	root = t.TempDir()
	docs = filepath.Join(root, "docs")
	require.NoError(t, os.MkdirAll(docs, 0o755))

	before, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(before) })

	// t.TempDir may hand back a path through a symlink (/tmp on macOS), and the
	// boundary is compared after resolving them.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	return root, docs
}

// TestAttachmentOutsideTheProjectIsRefused: a document says which files to
// upload, and a document is content. On a repository that takes pull requests a
// contributor could point an image at a deploy key or a credentials file and
// have its contents published as a page attachment -- under a flattened name
// that looks like any other.
func TestAttachmentOutsideTheProjectIsRefused(t *testing.T) {
	root, docs := project(t)

	secret := filepath.Join(filepath.Dir(root), "id_rsa")
	require.NoError(t, os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600))

	_, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"../../id_rsa"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOutsideProject)
	assert.Contains(t, err.Error(), "id_rsa", "the message names the file")
}

// TestSymlinkOutOfTheProjectIsRefused: a link committed to the repository
// reaches outside it as well as a path does, and the path itself looks
// innocent, so the check is made after resolving links.
func TestSymlinkOutOfTheProjectIsRefused(t *testing.T) {
	root, docs := project(t)

	secret := filepath.Join(filepath.Dir(root), "credentials")
	require.NoError(t, os.WriteFile(secret, []byte("aws_secret_access_key = x"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(docs, "logo.png")))

	_, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"logo.png"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOutsideProject)
}

// TestUpwardPathInsideTheProjectStillWorks is the layout README documents: a
// docs directory referring to shared assets beside it. Refusing upward paths
// outright would have broken it.
func TestUpwardPathInsideTheProjectStillWorks(t *testing.T) {
	root, docs := project(t)

	images := filepath.Join(root, "images")
	require.NoError(t, os.MkdirAll(images, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(images, "logo.png"), []byte("png"), 0o600))

	attachments, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"../images/logo.png"})

	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, "../images/logo.png", attachments[0].Replace)
}

// TestAttachmentBesideTheDocumentStillWorks covers a document published from
// outside the working directory, which is what a run given an absolute path
// does -- the document's own directory is a boundary of its own.
func TestAttachmentBesideTheDocumentStillWorks(t *testing.T) {
	elsewhere := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "logo.png"), []byte("png"), 0o600))

	attachments, err := ResolveLocalAttachments(vfs.LocalOS, elsewhere, []string{"logo.png"})

	require.NoError(t, err)
	require.Len(t, attachments, 1)
}
