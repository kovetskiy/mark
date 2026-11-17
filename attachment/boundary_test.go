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

// boundaryProject lays out a repository holding two documents' directories, each
// with a file of its own, and a file outside the repository, and runs from the
// repository as a publish does.
func boundaryProject(t *testing.T) (repo, docs, outside string) {
	t.Helper()

	repo, docs = project(t)

	write := func(dir, name, content string) {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	write(filepath.Join(docs, "images"), "logo.png", "logo")
	write(docs, "notes.pdf", "notes")
	write(filepath.Join(repo, "other-doc"), "file.pdf", "other")

	outside = filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-outside")
	write(outside, "outside.pdf", "outside")
	t.Cleanup(func() { _ = os.RemoveAll(outside) })

	return repo, docs, outside
}

// TestBoundaryDefaultAllowsAnotherDocumentsFile: the zero Boundary is the one
// mark has always drawn, which lets a document reach a file beside another
// document because it is inside the working directory.
func TestBoundaryDefaultAllowsAnotherDocumentsFile(t *testing.T) {
	_, docs, _ := boundaryProject(t)

	for _, opener := range []Opener{LocalOS, Root{}, Root{}.WithBoundary("")} {
		got, err := ResolveLocalAttachments(opener, docs, []string{"../other-doc/file.pdf"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "other", string(got[0].FileBytes))
	}

	require.NoError(t, Root{}.CheckReadable(docs, "../other-doc/file.pdf"))
}

// TestBoundaryDocumentRefusesOutsideTheDocumentsDirectory: BoundaryDocument
// holds a document to its own directory, so another document's file is
// refused like one outside the repository, by path, by pattern and by symlink.
func TestBoundaryDocumentRefusesOutsideTheDocumentsDirectory(t *testing.T) {
	repo, docs, _ := boundaryProject(t)

	require.NoError(t, os.Symlink(filepath.Join(repo, "other-doc", "file.pdf"), filepath.Join(docs, "linked.pdf")))

	root := Root{}.WithBoundary(BoundaryDocument)
	assert.Equal(t, BoundaryDocument, root.Boundary())

	for _, name := range []string{"../other-doc/file.pdf", "../other-doc/*.pdf", "linked.pdf"} {
		_, err := ResolveLocalAttachments(root, docs, []string{name})
		require.ErrorIs(t, err, ErrOutsideProject, name)
		assert.Contains(t, err.Error(), "the document's directory "+`"`+docs+`": `+boundarySetting, name)

		_, err = ResolveLocalAttachment(root, docs, name)
		require.ErrorIs(t, err, ErrOutsideProject, name)

		require.ErrorIs(t, root.CheckReadable(docs, name), ErrOutsideProject, name)
	}
}

// TestBoundaryRefusalNamesTheSettingAndTheDirectory: run from the document's
// own directory the base is ".", which tells the reader neither where the
// boundary is nor which setting drew it, so the refusal gives the directory
// resolved and names the setting, whether the default boundary or the
// attachment root refused the file.
func TestBoundaryRefusalNamesTheSettingAndTheDirectory(t *testing.T) {
	repo, docs, _ := boundaryProject(t)
	t.Chdir(docs)

	file := filepath.Join(repo, "other-doc", "file.pdf")

	err := Root{}.WithBoundary(BoundaryDocument).CheckReadable(".", "../other-doc/file.pdf")
	require.ErrorIs(t, err, ErrOutsideProject)
	assert.Equal(t, ErrOutsideProject.Error()+`: "../other-doc/file.pdf" resolves to `+file+
		`, which is outside both "`+docs+`" and the directory mark is running in, the default boundary, `+
		`which Rendering.AttachmentRoot (--attachment-root) "document" narrows and never widens; `+
		`publish from a directory that contains it`, err.Error())

	err = Root{}.WithBoundary("images").CheckReadable(".", "notes.pdf")
	require.ErrorIs(t, err, ErrOutsideProject)
	assert.Equal(t, ErrOutsideProject.Error()+`: "notes.pdf" resolves to `+filepath.Join(docs, "notes.pdf")+
		`, which is outside "`+filepath.Join(docs, "images")+
		`", the directory Rendering.AttachmentRoot (--attachment-root) names`, err.Error())

	_, err = ResolveLocalAttachment(Root{}.WithBoundary(BoundaryDocument), ".", "../other-doc/file.pdf")
	require.ErrorIs(t, err, ErrOutsideProject)
	assert.Contains(t, err.Error(), `both "`+docs+`"`)
}

// TestBoundaryDocumentAllowsBelowTheDocument: the document's directory and
// everything under it stay readable, a symlink that stays inside included.
func TestBoundaryDocumentAllowsBelowTheDocument(t *testing.T) {
	_, docs, _ := boundaryProject(t)

	require.NoError(t, os.Symlink(filepath.Join("images", "logo.png"), filepath.Join(docs, "alias.png")))

	root := Root{}.WithBoundary(BoundaryDocument)

	got, err := ResolveLocalAttachments(root, docs,
		[]string{"notes.pdf", "images/logo.png", "./images/../notes.pdf", "images/*.png", "alias.png"})
	require.NoError(t, err)
	assert.Len(t, got, 5)

	require.NoError(t, root.CheckReadable(docs, filepath.Join(docs, "images", "logo.png")))
}

// TestBoundaryPathHoldsEveryDocumentToIt: a directory as the Boundary allows
// what is inside it, whichever document names it, and refuses what is not, the
// document's own directory included when the root does not hold it.
func TestBoundaryPathHoldsEveryDocumentToIt(t *testing.T) {
	repo, docs, outside := boundaryProject(t)

	root := Root{}.WithBoundary(Boundary(docs))

	got, err := ResolveLocalAttachments(root, docs, []string{"images/logo.png"})
	require.NoError(t, err)
	assert.Len(t, got, 1)

	_, err = ResolveLocalAttachment(root, docs, "../other-doc/file.pdf")
	require.ErrorIs(t, err, ErrOutsideProject)
	assert.Contains(t, err.Error(), boundarySetting)

	// The other document's own file is outside a root that does not hold it.
	_, err = ResolveLocalAttachment(root, filepath.Join(repo, "other-doc"), "file.pdf")
	require.ErrorIs(t, err, ErrOutsideProject)
	require.ErrorIs(t, root.CheckReadable(filepath.Join(repo, "other-doc"), "file.pdf"), ErrOutsideProject)

	// A relative root is taken from the working directory, the repository.
	_, err = ResolveLocalAttachment(Root{}.WithBoundary("."), docs, "../other-doc/file.pdf")
	require.NoError(t, err)

	beyond := filepath.Join("..", "..", filepath.Base(outside), "outside.pdf")

	_, err = ResolveLocalAttachment(Root{}.WithBoundary("."), docs, beyond)
	require.ErrorIs(t, err, ErrOutsideProject)
}

// TestBoundaryPathNarrowsAndNeverWidens: a directory root holds a file to the
// default boundary as well as to itself, wherever the directory lies: above
// the working directory, at it, inside it or beside it. A refusal says which
// of the two refused the file.
func TestBoundaryPathNarrowsAndNeverWidens(t *testing.T) {
	repo, docs, outside := boundaryProject(t)

	const (
		allowed     = "allowed"
		byDefault   = "the default boundary"
		byRoot      = "the attachment root"
		ownFile     = "notes.pdf"
		otherDoc    = "../other-doc/file.pdf"
		defaultText = "the default boundary, which " + boundarySetting
		rootText    = "the directory " + boundarySetting + " names"
	)

	beyond := filepath.Join("..", "..", filepath.Base(outside), "outside.pdf")
	absolute := filepath.Join(outside, "outside.pdf")

	cases := []struct {
		name  string
		root  Boundary
		wants map[string]string
	}{
		{"above", Boundary(filepath.Dir(repo)), map[string]string{
			ownFile: allowed, otherDoc: allowed, beyond: byDefault,
		}},
		{"above, relative", "..", map[string]string{
			ownFile: allowed, otherDoc: allowed, beyond: byDefault,
		}},
		{"the filesystem's root", Boundary(string(filepath.Separator)), map[string]string{
			ownFile: allowed, otherDoc: allowed, beyond: byDefault,
		}},
		{"equal", Boundary(repo), map[string]string{
			ownFile: allowed, otherDoc: allowed, beyond: byDefault,
		}},
		{"inside", Boundary(docs), map[string]string{
			ownFile: allowed, otherDoc: byRoot, beyond: byDefault,
		}},
		{"beside", Boundary(outside), map[string]string{
			ownFile: byRoot, otherDoc: byRoot, beyond: byDefault,
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := Root{}.WithBoundary(c.root)

			for name, want := range c.wants {
				_, resolveErr := ResolveLocalAttachment(root, docs, name)
				checkErr := root.CheckReadable(docs, name)

				for _, err := range []error{resolveErr, checkErr} {
					switch want {
					case allowed:
						require.NoError(t, err, name)
					case byDefault:
						require.ErrorIs(t, err, ErrOutsideProject, name)
						assert.Contains(t, err.Error(), defaultText, name)
					case byRoot:
						require.ErrorIs(t, err, ErrOutsideProject, name)
						assert.Contains(t, err.Error(), rootText, name)
					}
				}
			}

			// An attachment's absolute name is joined to the document's
			// directory; a reference's is read as itself.
			err := root.CheckReadable(docs, absolute)
			require.ErrorIs(t, err, ErrOutsideProject)
			assert.Contains(t, err.Error(), defaultText)
		})
	}
}

// boundaryMap is an fs.FS holding two documents' directories, the guide's
// with a symbolic link to the other's file and one that stays inside.
func boundaryMap() fstest.MapFS {
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}

	return fstest.MapFS{
		"guide/index.md":       {Data: []byte("guide")},
		"guide/images/a.png":   {Data: []byte("a")},
		"guide/alias.png":      link("images/a.png"),
		"guide/linked.pdf":     link("../other-doc/file.pdf"),
		"guide/other":          link("../other-doc"),
		"other-doc/file.pdf":   {Data: []byte("other")},
		"other-doc/images.png": {Data: []byte("other image")},
	}
}

// TestFSRootBoundaryDocument: BoundaryDocument narrows a Root reading from an
// fs.FS to the document's directory inside it, through the links in it, where
// the zero Boundary allows the whole fs.FS.
func TestFSRootBoundaryDocument(t *testing.T) {
	wide := FSRoot(boundaryMap())
	narrow := wide.WithBoundary(BoundaryDocument)

	for _, name := range []string{"../other-doc/file.pdf", "../other-doc/*.pdf", "linked.pdf", "other/file.pdf"} {
		_, err := ResolveLocalAttachments(wide, "guide", []string{name})
		require.NoError(t, err, "the fs.FS is the default boundary: %s", name)
		require.NoError(t, wide.CheckReadable("guide", name), name)

		_, err = ResolveLocalAttachments(narrow, "guide", []string{name})
		require.ErrorIs(t, err, ErrOutsideProject, name)
		assert.Contains(t, err.Error(), boundarySetting, name)
		require.ErrorIs(t, narrow.CheckReadable("guide", name), ErrOutsideProject, name)
	}

	got, err := ResolveLocalAttachments(narrow, "guide", []string{"images/a.png", "alias.png", "images/*.png"})
	require.NoError(t, err)
	assert.Len(t, got, 3)
	require.NoError(t, narrow.CheckReadable("guide", "images/a.png"))

	// A document at the root of the fs.FS holds all of it.
	_, err = ResolveLocalAttachment(narrow, ".", "other-doc/file.pdf")
	require.NoError(t, err)
}

// TestFSRootBoundaryRefusesTheIncludePath: a name under --include-path is on
// disk, outside the document's directory in its fs.FS, so BoundaryDocument
// refuses it, though the include path alone allows it.
func TestFSRootBoundaryRefusesTheIncludePath(t *testing.T) {
	include := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(include, "shared.png"), []byte("shared"), 0o600))

	root := FSRoot(boundaryMap()).WithIncludePath(include)
	name, _ := root.IncludeName("shared.png")

	require.NoError(t, root.CheckReadable("guide", name))
	require.ErrorIs(t, root.WithBoundary(BoundaryDocument).CheckReadable("guide", name), ErrOutsideProject)
}

// TestFSRootBoundaryDirectoryHoldsNothing: a directory Boundary is a
// directory on disk, so a Root reading from an fs.FS refuses every file under
// it rather than reading one it was never compared with.
func TestFSRootBoundaryDirectoryHoldsNothing(t *testing.T) {
	root := FSRoot(boundaryMap()).WithBoundary(Boundary(t.TempDir()))

	_, err := ResolveLocalAttachment(root, "guide", "images/a.png")
	require.ErrorIs(t, err, ErrOutsideProject)
	assert.Contains(t, err.Error(), "a directory on disk")
	require.ErrorIs(t, root.CheckReadable("guide", "images/a.png"), ErrOutsideProject)
}

// TestBoundaryValidate: a directory root has to exist and be a directory; the
// zero value and BoundaryDocument need nothing on disk.
func TestBoundaryValidate(t *testing.T) {
	_, docs, _ := boundaryProject(t)

	require.NoError(t, Boundary("").Validate())
	require.NoError(t, BoundaryDocument.Validate())
	require.NoError(t, Boundary(docs).Validate())

	err := Boundary(filepath.Join(docs, "missing")).Validate()
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "is not a usable directory")

	assert.ErrorContains(t, Boundary(filepath.Join(docs, "notes.pdf")).Validate(), "is not a directory")
}

// TestBoundaryDocumentThroughASymlinkedWorkingDirectory: run from a
// repository reached through a symlink, a document named relative to it
// reaches a file in its own directory by an absolute name too, as a d2
// import may, under BoundaryDocument as under the default boundary.
func TestBoundaryDocumentThroughASymlinkedWorkingDirectory(t *testing.T) {
	_, docs, _ := boundaryProject(t)

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(filepath.Dir(docs), link))
	t.Chdir(link)
	t.Setenv("PWD", link)

	absolute := filepath.Join(link, "docs", "notes.pdf")
	for _, boundary := range []Boundary{"", BoundaryDocument} {
		require.NoError(t, Root{}.WithBoundary(boundary).CheckReadable("docs", absolute), "boundary %q", boundary)
	}
}
