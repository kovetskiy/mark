package attachment

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSRootResolvesAttachmentsInsideIt(t *testing.T) {
	root := FSRoot(fstest.MapFS{
		"docs/img/a.png":  {Data: []byte("a")},
		"docs/img/b.png":  {Data: []byte("b")},
		"shared/data.txt": {Data: []byte("data")},
	})

	found, err := ResolveLocalAttachments(root, "docs", []string{"img/*.png", "../shared/data.txt"})
	require.NoError(t, err)

	var names []string
	for _, attached := range found {
		names = append(names, attached.Name)
	}
	assert.Equal(t, []string{"img/a.png", "img/b.png", "../shared/data.txt"}, names)
	assert.Equal(t, []byte("data"), found[2].FileBytes)
}

func TestFSRootRefusesWhatIsOutsideIt(t *testing.T) {
	root := FSRoot(fstest.MapFS{"x.png": {Data: []byte("x")}})

	for _, name := range []string{"../../x.png", "../../*.png"} {
		_, err := ResolveLocalAttachments(root, "docs", []string{name})
		require.ErrorIs(t, err, ErrOutsideProject, name)
	}

	require.ErrorIs(t, root.CheckReadable("docs", "../../x.png"), ErrOutsideProject)
	require.ErrorIs(t, root.CheckReadable("docs", "/x.png"), ErrOutsideProject)
	require.NoError(t, root.CheckReadable("docs", "../x.png"))

	_, err := root.Stat("../x.png")
	require.Error(t, err)
}
