package mermaid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onePixel is a PNG small enough to write into a diagram as a data: URL.
const onePixel = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// imageServer counts the requests that reach it.
func imageServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

func imageShape(src string) string {
	return "flowchart TD\n A@{ img: \"" + src + "\", label: \"x\", pos: \"t\", w: 60, h: 60, constraint: \"off\" }\n"
}

// TestImageShapeWithARemoteImageSaysWhy: an image shape names its picture by
// URL, and the browser diagrams are drawn in loads nothing from the network,
// so the diagram fails with an error that names the URL and the reason.
func TestImageShapeWithARemoteImageSaysWhy(t *testing.T) {
	server, requests := imageServer(t)

	for _, output := range []imageformat.Format{imageformat.PNG, imageformat.SVG} {
		for _, url := range []string{
			server.URL + "/shape.png",
			strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/shape.png",
		} {
			_, err := engine.Render(context.Background(), "remote", []byte(imageShape(url)), Options{Output: output})
			require.Error(t, err, url)
			require.ErrorIs(t, err, chrome.ErrNetworkUnavailable)
			assert.Contains(t, err.Error(), url)
			assert.Contains(t, err.Error(), "data: URL")
		}
	}

	assert.Zero(t, requests.Load(), "the browser should have requested nothing")
}

// TestImageLabelWithARemoteImage: an <img> in a label is drawn as nothing when
// its picture does not load, so a PNG, which would be missing it, is refused,
// while an SVG, which keeps the address for whoever opens it, is published.
func TestImageLabelWithARemoteImage(t *testing.T) {
	server, requests := imageServer(t)

	diagram := []byte("flowchart TD\n A[\"<img src='" + server.URL + "/label.png'> label\"]\n")

	_, err := engine.Render(context.Background(), "remote", diagram, Options{})
	require.ErrorIs(t, err, chrome.ErrNetworkUnavailable)
	assert.Contains(t, err.Error(), server.URL+"/label.png")
	assert.Contains(t, err.Error(), "--mermaid-output=svg")

	svg, err := engine.Render(context.Background(), "remote", diagram, Options{Output: imageformat.SVG})
	require.NoError(t, err)
	assert.Contains(t, string(svg.FileBytes), server.URL+"/label.png")

	assert.Zero(t, requests.Load(), "the browser should have requested nothing")

	// The engine is still good for the next diagram.
	_, err = engine.Render(context.Background(), "after", []byte("graph TD;\n A-->B;"), Options{})
	require.NoError(t, err)
}

// TestImageShapeWithADataURL: a picture carried by the diagram itself needs no
// network, and draws as it always has.
func TestImageShapeWithADataURL(t *testing.T) {
	for _, output := range []imageformat.Format{imageformat.PNG, imageformat.SVG} {
		got, err := engine.Render(context.Background(), "inline", []byte(imageShape(onePixel)), Options{Output: output})
		require.NoError(t, err)
		assert.NotEmpty(t, got.FileBytes)
	}
}
