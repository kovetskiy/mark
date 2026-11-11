package attachment

import (
	"io/fs"
	"os"
	"path/filepath"
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

// linkedMap is a file system holding img/a.png and img/b.png, and symbolic
// links under docs to places inside it, outside it, and round in a loop.
func linkedMap() fstest.MapFS {
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}

	return fstest.MapFS{
		"img/a.png":      {Data: []byte("a")},
		"img/b.png":      {Data: []byte("b")},
		"docs/a.png":     link("../img/a.png"),
		"docs/imgs":      link("../img"),
		"docs/first":     link("second"),
		"docs/second":    link("imgs/b.png"),
		"docs/up.png":    link("../../x.png"),
		"docs/abs.png":   link("/etc/passwd"),
		"docs/updir":     link(".."),
		"docs/outdir":    link("../.."),
		"docs/loop":      link("loop2"),
		"docs/loop2":     link("loop"),
		"docs/via/x.png": {Data: []byte("x")},
	}
}

func TestFSRootFollowsLinksInsideIt(t *testing.T) {
	root := FSRoot(linkedMap())

	found, err := ResolveLocalAttachments(root, "docs", []string{"a.png", "first", "imgs/*.png", "updir/img/a.png"})
	require.NoError(t, err)

	var contents []string
	for _, attached := range found {
		contents = append(contents, string(attached.FileBytes))
	}
	assert.Equal(t, []string{"a", "b", "a", "b", "a"}, contents)

	data, err := root.ReadFile("docs/imgs/a.png")
	require.NoError(t, err)
	assert.Equal(t, []byte("a"), data)

	info, err := root.Stat("docs/imgs")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFSRootRefusesLinksOutOfIt(t *testing.T) {
	root := FSRoot(linkedMap())

	for _, name := range []string{"up.png", "abs.png", "outdir/x.png", "outdir/*.png"} {
		_, err := ResolveLocalAttachments(root, "docs", []string{name})
		require.ErrorIs(t, err, ErrOutsideProject, name)
		require.ErrorIs(t, root.CheckReadable("docs", name), ErrOutsideProject, name)
	}

	_, err := root.ReadFile("docs/up.png")
	require.ErrorIs(t, err, ErrOutsideProject)
	_, err = root.Open("docs/abs.png")
	require.ErrorIs(t, err, ErrOutsideProject)
	_, err = root.Stat("docs/outdir")
	require.ErrorIs(t, err, ErrOutsideProject)
}

func TestFSRootRefusesALinkLoop(t *testing.T) {
	root := FSRoot(linkedMap())

	_, err := root.ReadFile("docs/loop")
	require.ErrorContains(t, err, "too many levels of symbolic links")
	require.NotErrorIs(t, err, ErrOutsideProject)
}

// TestFSRootGlobSkipsLinksOutOfIt: a pattern walking the FS does not walk
// through a link out of it, and a match that is one is refused.
func TestFSRootGlobSkipsLinksOutOfIt(t *testing.T) {
	fsys := linkedMap()
	delete(fsys, "docs/updir") // a link to an ancestor, which ** would walk round
	root := FSRoot(fsys)

	found, err := ResolveLocalAttachments(root, ".", []string{"docs/**/x.png"})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "docs/via/x.png", found[0].Name)

	_, err = ResolveLocalAttachments(root, "docs", []string{"*.png"})
	require.ErrorIs(t, err, ErrOutsideProject)
}

// TestFSRootOverDirFSHoldsThroughLinks: os.DirFS follows a link out of its
// directory by itself; through FSRoot it does not.
func TestFSRootOverDirFSHoldsThroughLinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inside.txt"), []byte("inside"), 0o600))
	if err := os.Symlink(outside, filepath.Join(dir, "secret.txt")); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	require.NoError(t, os.Symlink("inside.txt", filepath.Join(dir, "alias.txt")))

	fsys := os.DirFS(dir)
	data, err := fs.ReadFile(fsys, "secret.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), data, "os.DirFS alone follows the link out")

	root := FSRoot(fsys)
	_, err = root.ReadFile("secret.txt")
	require.ErrorIs(t, err, ErrOutsideProject)

	data, err = root.ReadFile("alias.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("inside"), data)
}

// TestDiskRootFollowsLinks: the zero Root reads the disk as before, link and
// all; its containment is CheckReadable's.
func TestDiskRootFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inside.txt"), []byte("inside"), 0o600))
	if err := os.Symlink("inside.txt", filepath.Join(dir, "alias.txt")); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}

	data, err := Root{}.ReadFile(filepath.Join(dir, "alias.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("inside"), data)
	assert.Nil(t, Root{}.FS())
	assert.Nil(t, FSRoot(nil).FS())
}

// TestCheckWithinRelativeDirUnderLinkedWorkingDirectory: a relative dir is
// read against the working directory as the path is, though the working
// directory is reached through a symbolic link.
func TestCheckWithinRelativeDirUnderLinkedWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "inc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "inc", "partial.md"), []byte("x"), 0o600))

	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make a symbolic link: %v", err)
	}
	t.Chdir(link)

	assert.NoError(t, CheckWithin("inc", filepath.Join(link, "inc", "partial.md")))
	assert.NoError(t, CheckWithin(filepath.Join(link, "inc"), "inc/partial.md"))
	assert.ErrorIs(t, CheckWithin("inc", filepath.Join(link, "other.md")), ErrOutsideProject)
}
