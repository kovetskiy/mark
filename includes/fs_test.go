package includes

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncludesResolveInsideTheFS: a document in an fs.FS includes what is
// beside it there, a fragment includes what is beside the fragment, and
// --include-path is still read from disk.
func TestIncludesResolveInsideTheFS(t *testing.T) {
	includePath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(includePath, "shared.md"), []byte("from disk"), 0o600))

	root := attachment.FSRoot(fstest.MapFS{
		"guides/frag.md":        {Data: []byte("frag [<!-- Include: parts/inner.md -->]")},
		"guides/parts/inner.md": {Data: []byte("inner")},
	})

	_, out, _, err := ProcessIncludesIn(root, "guides", includePath,
		[]byte("<!-- Include: frag.md --> <!-- Include: shared.md -->"), template.New("test"), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "frag [inner] from disk", string(out))
}

func TestIncludesCannotLeaveTheFS(t *testing.T) {
	root := attachment.FSRoot(fstest.MapFS{"secret.md": {Data: []byte("secret")}})

	_, _, _, err := ProcessIncludesIn(root, "guides/deeper", "",
		[]byte("<!-- Include: ../../../secret.md -->"), template.New("test"), nil, nil)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
}
