package d2

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
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
			got, err := ProcessD2(tt.name, tt.markdown, tt.scale)
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

// TestProcessD2SVG covers the other thing a diagram can be published as: the
// drawing itself rather than a picture of it.
func TestProcessD2SVG(t *testing.T) {
	got, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0, false)
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

// TestProcessD2SVGScalesWhatThePageShowsAndNotTheFile covers what the scale
// means for an SVG. The drawing is the same at every size, so the file and the
// checksum that identifies it do not move; the size the page displays it at
// does.
func TestProcessD2SVGScalesWhatThePageShowsAndNotTheFile(t *testing.T) {
	plain, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0, false)
	require.NoError(t, err)

	scaled, err := ProcessD2SVG("example", []byte(diagram), "-", 2.0, false)
	require.NoError(t, err)

	assert.Equal(t, plain.Checksum, scaled.Checksum, "the drawing did not change")
	assert.Equal(t, plain.FileBytes, scaled.FileBytes)

	plainWidth, err := strconv.Atoi(plain.Width)
	require.NoError(t, err)

	scaledWidth, err := strconv.Atoi(scaled.Width)
	require.NoError(t, err)

	assert.Equal(t, plainWidth*2, scaledWidth, "the page shows it twice as wide")
}

// TestProcessD2SVGAndPNGAreDifferentAttachments pins that publishing the same
// diagram both ways cannot be mistaken for publishing it once.
func TestProcessD2SVGAndPNGAreDifferentAttachments(t *testing.T) {
	svg, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0, false)
	require.NoError(t, err)

	png, err := ProcessD2("example", []byte(diagram), 1.0)
	require.NoError(t, err)

	assert.NotEqual(t, svg.Checksum, png.Checksum)
	assert.NotEqual(t, svg.Filename, png.Filename)
}

// TestProcessD2SVGInlinesWhatTheDiagramReferences covers the reason the SVG
// path bundles at all.
//
// Confluence serves the attachment from its own host, where a path relative to
// the document resolves to nothing: left as a reference, an icon beside the
// document is a hole in the published diagram. The file has to carry it.
func TestProcessD2SVGInlinesWhatTheDiagramReferences(t *testing.T) {
	dir := t.TempDir()

	// The bundler reads the file to inline it, so this has to be a real one.
	icon := filepath.Join(dir, "icon.png")
	require.NoError(t, os.WriteFile(icon, onePixelPNG(t), 0o600))

	document := filepath.Join(dir, "doc.md")

	got, err := ProcessD2SVG("icons", []byte("a: {icon: ./icon.png}\na -> b\n"), document, 1.0, false)
	require.NoError(t, err)

	published := string(got.FileBytes)
	assert.Contains(t, published, "data:image/png;base64",
		"the icon should have been read into the file")
	assert.NotContains(t, published, "./icon.png",
		"and nothing should be left pointing at a path Confluence cannot follow")
}

// TestProcessD2SVGRefusesAReferenceItCannotRead is the other half: a diagram
// published with an icon missing looks like a diagram that was drawn that way,
// so the run stops instead.
func TestProcessD2SVGRefusesAReferenceItCannotRead(t *testing.T) {
	dir := t.TempDir()
	document := filepath.Join(dir, "doc.md")

	_, err := ProcessD2SVG("missing", []byte("a: {icon: ./nothing-here.png}\na -> b\n"), document, 1.0, false)
	require.Error(t, err)
}

// TestProcessD2SVGWillNotReadFromTheWorkingDirectory covers a diagram compiled
// without a document behind it, which CompileMarkdown does.
//
// A relative reference means nothing without a file to be relative to, and d2
// does not treat it as meaning nothing: given "-" it leaves the path as written
// and reads it from wherever mark happens to be running, so a diagram could
// name a file beside the process and publish what it found. It is refused.
func TestProcessD2SVGWillNotReadFromTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "private.png"), onePixelPNG(t), 0o600))

	t.Chdir(dir)

	// Both ways of saying there is no document: an empty path, and d2's own
	// spelling of standard input. "-" is the one that matters, because it is
	// the value that makes the bundler read from the working directory rather
	// than refuse.
	for _, path := range []string{"", "-"} {
		_, err := ProcessD2SVG("pathless", []byte("a: {icon: ./private.png}\na -> b\n"), path, 1.0, false)
		require.Error(t, err, "path %q", path)

		assert.Contains(t, err.Error(), "private.png")
		assert.Contains(t, err.Error(), "file on disk")
	}
}

// TestProcessD2SVGWithoutAPathIsFineWithNothingToResolve is the boundary: only
// a diagram that actually points at something local needs a document, so the
// ordinary pathless compile still works.
func TestProcessD2SVGWithoutAPathIsFineWithNothingToResolve(t *testing.T) {
	got, err := ProcessD2SVG("pathless", []byte("a -> b\n"), "", 1.0, false)
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

// TestProcessD2SVGWillNotFetchWhatItWasNotAskedTo covers the request a diagram
// can make of the machine publishing it.
//
// d2's bundler fetches every URL a drawing names, with a client that follows
// redirects and declines nothing -- not loopback, not link-local, not a cloud
// metadata address -- and publishes what comes back inside the SVG. A document
// nobody vetted, a pull request from a fork, would otherwise read a credential
// endpoint and upload the answer to Confluence.
func TestProcessD2SVGWillNotFetchWhatItWasNotAskedTo(t *testing.T) {
	var asked bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true

		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("SECRET"))
	}))
	defer server.Close()

	diagram := []byte("a: {icon: " + server.URL + "/latest/meta-data/creds}\na -> b\n")

	_, err := ProcessD2SVG("remote", diagram, "", 1.0, false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "--d2-bundle-remote")
	assert.False(t, asked, "nothing should have been requested")
}

// TestProcessD2SVGFetchesWhenAskedTo is the other half: the flag exists because
// Confluence cannot fetch a remote image out of an SVG itself, so a document
// whose diagrams are trusted still has a way to publish one.
func TestProcessD2SVGFetchesWhenAskedTo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG(t))
	}))
	defer server.Close()

	got, err := ProcessD2SVG("remote",
		[]byte("a: {icon: "+server.URL+"/icon.png}\na -> b\n"), "", 1.0, true)
	require.NoError(t, err)

	assert.Contains(t, string(got.FileBytes), "data:image/png;base64",
		"the icon should have been fetched and carried in the drawing")
}

// TestProcessD2SVGWillNotReadOutsideTheProject covers a file the document names
// but has no business naming: an absolute path, which d2's bundler reads
// exactly as written, and an upward one, which it cleans its way out with.
//
// Held to the boundary an attachment is held to, since a diagram naming a file
// is doing what a document does.
func TestProcessD2SVGWillNotReadOutsideTheProject(t *testing.T) {
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
		_, err := ProcessD2SVG("outside",
			[]byte("a: {icon: "+reference+"}\na -> b\n"), document, 1.0, false)
		require.Error(t, err, "reference %q", reference)

		assert.Contains(t, err.Error(), "outside")
	}
}
