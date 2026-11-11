package d2

import (
	"bytes"
	"context"
	"encoding/binary"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diagramFS holds a document at docs/doc.md, what its diagrams import and
// show, and a symbolic link out of the FS.
func diagramFS(t *testing.T) fstest.MapFS {
	t.Helper()

	return fstest.MapFS{
		"docs/parts/below.d2":  {Data: []byte("below\nnested: @nested\n")},
		"docs/parts/nested.d2": {Data: []byte("nestedlabel\n")},
		"shared/style.d2":      {Data: []byte("sharedlabel\n")},
		"docs/icon.png":        {Data: onePixelPNG(t)},
		"docs/escape.d2":       {Data: []byte("../../outside.d2"), Mode: fs.ModeSymlink},
		"docs/escape.png":      {Data: []byte("/etc/icon.png"), Mode: fs.ModeSymlink},
	}
}

// TestRenderInAnFSReadsImportsAndImagesFromIt: a diagram of a document in an
// fs.FS imports and shows what is inside the FS, around the document's path,
// and both outputs carry the image.
func TestRenderInAnFSReadsImportsAndImagesFromIt(t *testing.T) {
	// Nothing of the FS is on disk, here or anywhere.
	t.Chdir(t.TempDir())

	files := diagramFS(t)
	source := []byte("a: @parts/below\nb: {icon: ./icon.png}\nc: @../shared/style\na -> b -> c\n")
	options := Options{InputPath: "docs/doc.md", Root: attachment.FSRoot(files), Scale: 1.0}

	svg := options
	svg.Output = imageformat.SVG
	got, err := Render(context.Background(), "fs", source, svg)
	require.NoError(t, err)

	published := string(got.FileBytes)
	for _, label := range []string{"below", "nestedlabel", "sharedlabel"} {
		assert.Contains(t, published, label)
	}
	assert.Contains(t, published, "data:image/png;base64", "the icon is read into the file")
	assert.NotContains(t, published, "icon.png", "and nothing points at a path Confluence cannot follow")

	png := options
	png.Output = imageformat.PNG
	png.Browser = browser
	before, err := Render(context.Background(), "", source, png)
	require.NoError(t, err)
	assert.NotEmpty(t, before.FileBytes)

	// The PNG's checksum covers the image it inlined, and the imports.
	changed := diagramFS(t)
	changed["docs/icon.png"] = &fstest.MapFile{Data: append(onePixelPNG(t), 0)}
	png.Root = attachment.FSRoot(changed)
	afterImage, err := Render(context.Background(), "", source, png)
	require.NoError(t, err)
	assert.NotEqual(t, before.Checksum, afterImage.Checksum, "a changed image changes the checksum")

	changed = diagramFS(t)
	changed["shared/style.d2"] = &fstest.MapFile{Data: []byte("othershared\n")}
	png.Root = attachment.FSRoot(changed)
	afterImport, err := Render(context.Background(), "", source, png)
	require.NoError(t, err)
	assert.NotEqual(t, before.Checksum, afterImport.Checksum, "a changed import changes the checksum")
}

// TestRenderInAnFSRefusesWhatClimbsOut: an import or image above the FS's
// root, absolute, or behind a symbolic link out of it is refused, and nothing
// is read from disk in its place.
func TestRenderInAnFSRefusesWhatClimbsOut(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.d2"), []byte("outside\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.png"), onePixelPNG(t), 0o600))

	sources := map[string]string{
		"an import above the root":   "x: @../../outside\n",
		"an absolute import":         `x: @"` + filepath.ToSlash(filepath.Join(dir, "outside")) + `"` + "\n",
		"an import linked out":       "x: @escape\n",
		"an image above the root":    "x: {icon: ../../outside.png}\n",
		"an absolute image":          "x: {icon: " + filepath.ToSlash(filepath.Join(dir, "outside.png")) + "}\n",
		"an image linked out":        "x: {icon: ./escape.png}\n",
		"a nested import above root": "x: @parts/climb\n",
	}

	files := diagramFS(t)
	files["docs/parts/climb.d2"] = &fstest.MapFile{Data: []byte("y: @../../../outside\n")}

	for name, source := range sources {
		for _, output := range []imageformat.Format{imageformat.SVG, imageformat.PNG} {
			t.Run(name+" as "+string(output), func(t *testing.T) {
				got, err := Render(context.Background(), "outside", []byte(source), Options{
					Output: output, Browser: browser, InputPath: "docs/doc.md",
					Root: attachment.FSRoot(files), Scale: 1.0,
				})
				require.Error(t, err)
				assert.ErrorIs(t, err, attachment.ErrOutsideProject)
				assert.Empty(t, got.FileBytes)
			})
		}
	}
}

// TestRenderPNGOnDiskLeavesALocalImageAlone: a document on disk draws a PNG as
// it did before documents could be in an fs.FS, with the checksum of its
// source and scale alone.
func TestRenderPNGOnDiskLeavesALocalImageAlone(t *testing.T) {
	docs, document, _ := importProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(docs, "icon.png"), onePixelPNG(t), 0o600))

	source := []byte("a: {icon: ./icon.png}\n")
	got, err := Render(context.Background(), "", source, Options{
		Output: imageformat.PNG, Browser: browser, InputPath: document, Scale: 1.0,
	})
	require.NoError(t, err)

	want, err := attachment.GetChecksum(bytes.NewReader(slices.Concat(source, binary.LittleEndian.AppendUint64(nil, math.Float64bits(1.0)))))
	require.NoError(t, err)
	assert.Equal(t, want, got.Checksum)
}
