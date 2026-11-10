package d2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var diagram string = `d2
vars: {
  d2-config: {
    layout-engine: elk
    # Terminal theme code
    theme-id: 300
  } 
}
network: {
  cell tower: {
    satellites: {
      shape: stored_data
      style.multiple: true
    }

    transmitter

    satellites -> transmitter: send
    satellites -> transmitter: send
    satellites -> transmitter: send
  }

  online portal: {
    ui: {shape: hexagon}
  }   
      
  data processor: {
    storage: {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: phone logs
}

user: {
  shape: person
  width: 130
}

user -> network.cell tower: make call
user -> network.online portal.ui: access {
  style.stroke-dash: 3
}   

api server -> network.online portal.ui: display
api server -> logs: persist
logs: {shape: page; style.multiple: true}

network.data processor -> api server
`

func TestExtractD2Image(t *testing.T) {
	tests := []struct {
		name     string
		markdown []byte
		scale    float64
		want     attachment.Attachment
		wantErr  assert.ErrorAssertionFunc
	}{
		{"example", []byte(diagram), 1.0, attachment.Attachment{
			// This is only the PNG Magic Header
			FileBytes: []byte{0x89, 0x50, 0x4e, 0x47, 0xd, 0xa, 0x1a, 0xa},
			Filename:  "example.png",
			Name:      "example",
			Replace:   "example",
			Checksum:  "40e75f93e09da9242d4b1ab8e2892665ec7d5bd1ac78a4b65210ee219cf62297",
			ID:        "",
		},
			assert.NoError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(context.Background(), tt.name, tt.markdown, Options{Browser: browser, Scale: tt.scale})
			if !tt.wantErr(t, err, fmt.Sprintf("processD2(%v, %v)", tt.name, string(tt.markdown))) {
				return
			}
			assert.Equal(t, tt.want.Filename, got.Filename, "processD2(%v, %v)", tt.name, string(tt.markdown))
			// We only test for the header as png changes based on system png library
			assert.Equal(t, tt.want.FileBytes, got.FileBytes[0:8], "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Name, got.Name, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Replace, got.Replace, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Checksum, got.Checksum, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.ID, got.ID, "processD2(%v, %v)", tt.name, string(tt.markdown))
			gotWidth, widthErr := strconv.ParseInt(got.Width, 10, 64)
			assert.NoError(t, widthErr, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotWidth, int64(0), "processD2(%v, %v)", tt.name, string(tt.markdown))

			gotHeight, heightErr := strconv.ParseInt(got.Height, 10, 64)
			assert.NoError(t, heightErr, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotHeight, int64(0), "processD2(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}

// TestRenderSVG covers the other thing a diagram can be published as: the
// drawing itself rather than a picture of it.
func TestRenderSVG(t *testing.T) {
	got, err := Render(context.Background(), "example", []byte(diagram), Options{Output: imageformat.SVG, InputPath: "-", Scale: 1.0})
	require.NoError(t, err)

	assert.Equal(t, "example.svg", got.Filename)
	assert.Equal(t, "example", got.Name)
	assert.Equal(t, "example", got.Replace)
	assert.True(t, strings.Contains(string(got.FileBytes), "<svg"),
		"the attachment should be an SVG document")

	width, err := strconv.Atoi(got.Width)
	require.NoError(t, err, "width should be a whole number of pixels")
	assert.Positive(t, width)

	height, err := strconv.Atoi(got.Height)
	require.NoError(t, err, "height should be a whole number of pixels")
	assert.Positive(t, height)
}

// TestRenderSVGScalesWhatThePageShowsAndNotTheFile covers what the scale
// means for an SVG. The drawing is the same at every size, so the file and the
// checksum that identifies it do not move; the size the page displays it at
// does.
func TestRenderSVGScalesWhatThePageShowsAndNotTheFile(t *testing.T) {
	plain, err := Render(context.Background(), "example", []byte(diagram), Options{Output: imageformat.SVG, InputPath: "-", Scale: 1.0})
	require.NoError(t, err)

	scaled, err := Render(context.Background(), "example", []byte(diagram), Options{Output: imageformat.SVG, InputPath: "-", Scale: 2.0})
	require.NoError(t, err)

	assert.Equal(t, plain.Checksum, scaled.Checksum, "the drawing did not change")
	assert.Equal(t, plain.FileBytes, scaled.FileBytes)

	plainWidth, err := strconv.Atoi(plain.Width)
	require.NoError(t, err)

	scaledWidth, err := strconv.Atoi(scaled.Width)
	require.NoError(t, err)

	assert.Equal(t, plainWidth*2, scaledWidth, "the page shows it twice as wide")
}

// TestRenderSVGAndPNGAreDifferentAttachments pins that publishing the same
// diagram both ways cannot be mistaken for publishing it once.
func TestRenderSVGAndPNGAreDifferentAttachments(t *testing.T) {
	svg, err := Render(context.Background(), "example", []byte(diagram), Options{Output: imageformat.SVG, InputPath: "-", Scale: 1.0})
	require.NoError(t, err)

	png, err := Render(context.Background(), "example", []byte(diagram), Options{Browser: browser, Scale: 1.0})
	require.NoError(t, err)

	assert.NotEqual(t, svg.Checksum, png.Checksum)
	assert.NotEqual(t, svg.Filename, png.Filename)
}

// TestRenderSVGInlinesWhatTheDiagramReferences covers the reason the SVG
// path bundles at all.
//
// Confluence serves the attachment from its own host, where a path relative to
// the document resolves to nothing: left as a reference, an icon beside the
// document is a hole in the published diagram. The file has to carry it.
func TestRenderSVGInlinesWhatTheDiagramReferences(t *testing.T) {
	dir := t.TempDir()

	// The bundler reads the file to inline it, so this has to be a real one.
	icon := filepath.Join(dir, "icon.png")
	require.NoError(t, os.WriteFile(icon, onePixelPNG(t), 0o600))

	document := filepath.Join(dir, "doc.md")

	got, err := Render(context.Background(), "icons", []byte("a: {icon: ./icon.png}\na -> b\n"), Options{Output: imageformat.SVG, InputPath: document, Scale: 1.0})
	require.NoError(t, err)

	published := string(got.FileBytes)
	assert.Contains(t, published, "data:image/png;base64",
		"the icon should have been read into the file")
	assert.NotContains(t, published, "./icon.png",
		"and nothing should be left pointing at a path Confluence cannot follow")
}

// TestRenderSVGRefusesAReferenceItCannotRead is the other half: a diagram
// published with an icon missing looks like a diagram that was drawn that way,
// so the run stops instead.
func TestRenderSVGRefusesAReferenceItCannotRead(t *testing.T) {
	dir := t.TempDir()
	document := filepath.Join(dir, "doc.md")

	_, err := Render(context.Background(), "missing", []byte("a: {icon: ./nothing-here.png}\na -> b\n"), Options{Output: imageformat.SVG, InputPath: document, Scale: 1.0})
	require.Error(t, err)
}

// TestRenderSVGWillNotReadFromTheWorkingDirectory covers a diagram compiled
// without a document behind it, which CompileMarkdown does.
//
// A relative reference means nothing without a file to be relative to, and d2
// does not treat it as meaning nothing: given "-" it leaves the path as written
// and reads it from wherever mark happens to be running, so a diagram could
// name a file beside the process and publish what it found. It is refused.
func TestRenderSVGWillNotReadFromTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "private.png"), onePixelPNG(t), 0o600))

	t.Chdir(dir)

	// Both ways of saying there is no document: an empty path, and d2's own
	// spelling of standard input. "-" is the one that matters, because it is
	// the value that makes the bundler read from the working directory rather
	// than refuse.
	for _, path := range []string{"", "-"} {
		_, err := Render(context.Background(), "pathless", []byte("a: {icon: ./private.png}\na -> b\n"), Options{Output: imageformat.SVG, InputPath: path, Scale: 1.0})
		require.Error(t, err, "path %q", path)

		assert.Contains(t, err.Error(), "private.png")
		assert.Contains(t, err.Error(), "file on disk")
	}
}

// TestRenderSVGWithoutAPathIsFineWithNothingToResolve is the boundary: only
// a diagram that actually points at something local needs a document, so the
// ordinary pathless compile still works.
func TestRenderSVGWithoutAPathIsFineWithNothingToResolve(t *testing.T) {
	got, err := Render(context.Background(), "pathless", []byte("a -> b\n"), Options{Output: imageformat.SVG, Scale: 1.0})
	require.NoError(t, err)

	assert.NotEmpty(t, got.FileBytes)
}

// onePixelPNG returns the smallest PNG there is, for a test that needs a file
// the bundler will accept rather than an image anybody looks at.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()

	const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	return decoded
}

// TestRenderSVGWillNotFetchWhatItWasNotAskedTo covers the request a diagram
// can make of the machine publishing it.
//
// d2's bundler fetches every URL a drawing names, with a client that follows
// redirects and declines nothing -- not loopback, not link-local, not a cloud
// metadata address -- and publishes what comes back inside the SVG. A document
// nobody vetted, a pull request from a fork, would otherwise read a credential
// endpoint and upload the answer to Confluence.
func TestRenderSVGWillNotFetchWhatItWasNotAskedTo(t *testing.T) {
	var asked bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true

		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("SECRET"))
	}))
	defer server.Close()

	diagram := []byte("a: {icon: " + server.URL + "/latest/meta-data/creds}\na -> b\n")

	_, err := Render(context.Background(), "remote", diagram, Options{Output: imageformat.SVG, Scale: 1.0})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "--d2-bundle-remote")
	assert.False(t, asked, "nothing should have been requested")
}

// TestRenderSVGFetchesWhenAskedTo is the other half: the flag exists because
// Confluence cannot fetch a remote image out of an SVG itself, so a document
// whose diagrams are trusted still has a way to publish one.
func TestRenderSVGFetchesWhenAskedTo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG(t))
	}))
	defer server.Close()

	got, err := Render(context.Background(), "remote", []byte("a: {icon: "+server.URL+"/icon.png}\na -> b\n"), Options{Output: imageformat.SVG, Scale: 1.0, BundleRemote: true})
	require.NoError(t, err)

	assert.Contains(t, string(got.FileBytes), "data:image/png;base64",
		"the icon should have been fetched and carried in the drawing")
}

// TestRenderSVGWillNotReadOutsideTheProject covers a file the document names
// but has no business naming: an absolute path, which d2's bundler reads
// exactly as written, and an upward one, which it cleans its way out with.
//
// Held to the boundary an attachment is held to, since a diagram naming a file
// is doing what a document does.
func TestRenderSVGWillNotReadOutsideTheProject(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_rsa")
	require.NoError(t, os.WriteFile(secret, onePixelPNG(t), 0o600))

	// A directory of its own, so that neither the document nor the working
	// directory contains the file.
	project := t.TempDir()
	document := filepath.Join(project, "docs", "doc.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(document), 0o755))

	t.Chdir(project)

	for _, reference := range []string{secret, "../../" + filepath.Base(outside) + "/id_rsa"} {
		// A drive letter reads as a URL scheme, so d2 refuses a Windows absolute
		// path as an address before the project boundary is consulted.
		refused := "outside"
		if filepath.IsAbs(reference) && runtime.GOOS == "windows" {
			reference, refused = filepath.ToSlash(reference), "address"
		}

		_, err := Render(context.Background(), "outside", []byte("a: {icon: "+reference+"}\na -> b\n"), Options{Output: imageformat.SVG, InputPath: document, Scale: 1.0})
		require.Error(t, err, "reference %q", reference)

		assert.Contains(t, err.Error(), refused)
	}
}

// TestRenderFlattensASlashInTheTitle: the page refers to an attachment by
// its filename with any "/" flattened, so the file has to be uploaded under
// that name too, or a diagram titled Auth/Login is a broken image.
func TestRenderFlattensASlashInTheTitle(t *testing.T) {
	got, err := Render(context.Background(), "Auth/Login", []byte(diagram), Options{Output: imageformat.SVG, InputPath: "-", Scale: 1.0})
	require.NoError(t, err)

	assert.Equal(t, "Auth_Login.svg", got.Filename)
}

// A PNG with no browser to draw it in is refused before the layout is done.
func TestRenderWithoutABrowser(t *testing.T) {
	_, err := Render(context.Background(), "nothing", []byte("a -> b"), Options{Scale: 1})
	assert.ErrorIs(t, err, chrome.ErrNoBrowser)
}

func TestRenderRefusesAnUnknownFormat(t *testing.T) {
	_, err := Render(t.Context(), "gif", []byte("a -> b"), Options{Output: "gif"})
	require.ErrorContains(t, err, `unknown d2 output "gif"`)
}

// importProject lays out a document in docs/ of a project, with the working
// directory at the project's root, and a directory beside the project that
// holds outside.d2.
func importProject(t *testing.T) (docs, document, outside string) {
	t.Helper()

	outside = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "outside.d2"), []byte("outside\n"), 0o600))

	project := t.TempDir()
	docs = filepath.Join(project, "docs")
	document = filepath.Join(docs, "doc.md")
	require.NoError(t, os.MkdirAll(filepath.Join(docs, "parts"), 0o755))

	t.Chdir(project)

	return docs, document, outside
}

// TestRenderImportsFromTheDocumentsDirectory covers the imports that stay
// inside the project: beside the document, below it, and one imported by
// another, which d2 reads relative to the file that imports it.
func TestRenderImportsFromTheDocumentsDirectory(t *testing.T) {
	docs, document, _ := importProject(t)

	require.NoError(t, os.WriteFile(filepath.Join(docs, "beside.d2"), []byte("beside\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "parts", "below.d2"), []byte("below\nnested: @nested\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "parts", "nested.d2"), []byte("nestedlabel\n"), 0o600))

	source := []byte("a: @beside\nb: @parts/below\na -> b\n")

	got, err := Render(context.Background(), "imports", source, Options{Output: imageformat.SVG, InputPath: document, Scale: 1.0})
	require.NoError(t, err)

	for _, label := range []string{"beside", "below", "nestedlabel"} {
		assert.Contains(t, string(got.FileBytes), label)
	}

	_, err = Render(context.Background(), "imports", source, Options{Output: imageformat.PNG, Browser: browser, InputPath: document, Scale: 1.0})
	require.NoError(t, err)
}

// TestRenderImportsFromTheWorkingDirectory: like an image, an import may climb
// out of the document's directory as long as it stays inside the one mark runs
// in.
func TestRenderImportsFromTheWorkingDirectory(t *testing.T) {
	docs, document, _ := importProject(t)

	shared := filepath.Join(filepath.Dir(docs), "shared")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(shared, "style.d2"), []byte("sharedlabel\n"), 0o600))

	got, err := Render(context.Background(), "shared", []byte("x: @../shared/style\n"), Options{Output: imageformat.SVG, InputPath: document, Scale: 1.0})
	require.NoError(t, err)
	assert.Contains(t, string(got.FileBytes), "sharedlabel")
}

// TestRenderPNGChecksumCoversImports: a PNG's checksum is taken over its
// source, and what the source imports is part of it, so changing only an
// imported file has to change the attachment.
func TestRenderPNGChecksumCoversImports(t *testing.T) {
	docs, document, _ := importProject(t)

	imported := filepath.Join(docs, "part.d2")
	source := []byte("x: @part\n")
	options := Options{Output: imageformat.PNG, Browser: browser, InputPath: document, Scale: 1.0}

	require.NoError(t, os.WriteFile(imported, []byte("first\n"), 0o600))
	before, err := Render(context.Background(), "", source, options)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(imported, []byte("second\n"), 0o600))
	after, err := Render(context.Background(), "", source, options)
	require.NoError(t, err)

	assert.NotEqual(t, before.Checksum, after.Checksum)

	// A diagram that imports nothing keeps the checksum it always had.
	plain, err := Render(context.Background(), "", []byte("a -> b"), options)
	require.NoError(t, err)

	want, err := attachment.GetChecksum(bytes.NewReader(slices.Concat([]byte("a -> b"), binary.LittleEndian.AppendUint64(nil, math.Float64bits(1.0)))))
	require.NoError(t, err)
	assert.Equal(t, want, plain.Checksum)
}

// TestRenderRefusesAnImportOutsideTheProject holds an import to the boundary
// an image or icon is held to: the document's directory or the one mark runs
// in, with symlinks followed.
func TestRenderRefusesAnImportOutsideTheProject(t *testing.T) {
	docs, document, outside := importProject(t)

	imports := []string{
		filepath.ToSlash(filepath.Join(outside, "outside")),
		"../../" + filepath.Base(outside) + "/outside",
	}
	if err := os.Symlink(filepath.Join(outside, "outside.d2"), filepath.Join(docs, "linked.d2")); err != nil {
		t.Logf("no symlink: %v", err)
	} else {
		imports = append(imports, "linked")
	}

	for _, imported := range imports {
		for _, output := range []imageformat.Format{imageformat.SVG, imageformat.PNG} {
			source := []byte(`x: @"` + imported + `"` + "\n")

			got, err := Render(context.Background(), "outside", source, Options{Output: output, Browser: browser, InputPath: document, Scale: 1.0})
			require.Error(t, err, "import %q as %s", imported, output)

			assert.ErrorIs(t, err, attachment.ErrOutsideProject)
			assert.Contains(t, err.Error(), imported)
			assert.Empty(t, got.FileBytes)
		}
	}
}

// TestRenderRefusesAnImportWithoutADocument: a relative import needs a file
// to be relative to, and a diagram compiled with no path has none.
func TestRenderRefusesAnImportWithoutADocument(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beside.d2"), []byte("beside\n"), 0o600))

	t.Chdir(dir)

	for _, path := range []string{"", "-"} {
		_, err := Render(context.Background(), "pathless", []byte("x: @beside\n"), Options{Output: imageformat.SVG, InputPath: path, Scale: 1.0})
		require.Error(t, err, "path %q", path)

		assert.Contains(t, err.Error(), "beside")
		assert.Contains(t, err.Error(), "file on disk")
	}
}
