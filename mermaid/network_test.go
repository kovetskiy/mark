package mermaid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onePixel is a PNG small enough to write into a diagram as a data: URL.
const onePixel = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// offlineEngine is a chrome engine with Chrome's own refusal, in some
// releases, of a request to the machine it runs on turned off, as in chrome's
// network tests: the image server listens on this machine, and what is tested
// is that mark's options keep the browser from every host. The features
// chromedp turns off are repeated, since a flag given twice is replaced.
func offlineEngine(t *testing.T) *Engine {
	t.Helper()

	e, err := NewEngine(EngineChrome)
	require.NoError(t, err)

	e.extraOptions = []chromedp.ExecAllocatorOption{chromedp.Flag("disable-features",
		"Translate,BlinkGenPropertyTrees,LocalNetworkAccessChecks")}
	t.Cleanup(e.Close)

	return e
}

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
	offline := offlineEngine(t)

	for _, output := range []imageformat.Format{imageformat.PNG, imageformat.SVG} {
		for _, url := range []string{
			server.URL + "/shape.png",
			strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/shape.png",
		} {
			_, err := offline.Render(context.Background(), "remote", []byte(imageShape(url)), Options{Output: output})
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
	offline := offlineEngine(t)

	diagram := []byte("flowchart TD\n A[\"<img src='" + server.URL + "/label.png'> label\"]\n")

	_, err := offline.Render(context.Background(), "remote", diagram, Options{})
	require.ErrorIs(t, err, chrome.ErrNetworkUnavailable)
	assert.Contains(t, err.Error(), server.URL+"/label.png")
	assert.Contains(t, err.Error(), "--mermaid-output=svg")

	svg, err := offline.Render(context.Background(), "remote", diagram, Options{Output: imageformat.SVG})
	require.NoError(t, err)
	assert.Contains(t, string(svg.FileBytes), server.URL+"/label.png")

	assert.Zero(t, requests.Load(), "the browser should have requested nothing")

	// The engine is still good for the next diagram.
	_, err = offline.Render(context.Background(), "after", []byte("graph TD;\n A-->B;"), Options{})
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

// TestLabelTextNamingAURL: a label that only says src=https://... as text has
// no image to miss, so it is published as a PNG.
func TestLabelTextNamingAURL(t *testing.T) {
	diagram := []byte("flowchart TD\n A[\"src=https://example.com/x.png\"]\n")

	got, err := engine.Render(context.Background(), "text", diagram, Options{})
	require.NoError(t, err)
	assert.NotEmpty(t, got.FileBytes)
}
