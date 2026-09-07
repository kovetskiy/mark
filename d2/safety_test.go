package d2

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiagramCannotRunSomethingOnTheMachinePublishing is the reason for all of
// this.
//
// A d2 label written as |md | is passed into the drawing as markup, and the PNG
// is taken by navigating a browser to that drawing as a document -- so the
// markup runs, on the machine doing the publishing, in a browser started with
// --no-sandbox. A diagram in a pull request could read anything the runner can
// reach and send it somewhere.
//
// The listener asserts the negative that matters: not merely that the run
// failed, but that nothing was ever requested.
func TestDiagramCannotRunSomethingOnTheMachinePublishing(t *testing.T) {
	var requested atomic.Bool

	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(true)
	}))
	defer listener.Close()

	diagram := []byte("a: |md\n  <script>setTimeout(function(){location.href=\"" +
		listener.URL + "/exfil\"},500)</script>\n|\n")

	_, err := ProcessD2("png", diagram, 1.0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsafeDiagram)

	_, err = ProcessD2SVG("svg", diagram, "", 1.0, false)
	require.Error(t, err, "and it is not uploaded for somebody else's browser either")
	assert.ErrorIs(t, err, ErrUnsafeDiagram)

	// Longer than the script waits, so that a failure to block it would show.
	time.Sleep(2 * time.Second)

	assert.False(t, requested.Load(), "nothing should have been requested")
}

// TestUnsafeDiagramsAreRefused covers the rest of what a label can carry. Some
// of these d2's own parser turns away first; the ones that reach the drawing
// are turned away here.
func TestUnsafeDiagramsAreRefused(t *testing.T) {
	for name, label := range map[string]string{
		"an iframe":        "a: |md\n  <iframe src=\"https://example.com\"></iframe>\n|\n",
		"an object":        "a: |md\n  <object data=\"https://example.com\"></object>\n|\n",
		"an embed":         "a: |md\n  <embed src=\"https://example.com\" />\n|\n",
		"an error handler": "a: |md\n  <img src=\"x.png\" onerror=\"alert(1)\" />\n|\n",
		"a load handler":   "a: |md\n  <svg onload=\"alert(1)\"></svg>\n|\n",
		"a javascript url": "a: |md\n  <a href=\"javascript:alert(1)\">click</a>\n|\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ProcessD2SVG("probe", []byte(label), "", 1.0, false)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnsafeDiagram)
		})
	}
}

// TestLabelsThatOnlyDrawAreStillPublished is the boundary, and the whole reason
// this is a check rather than a ban on markdown labels: what people actually
// write in them keeps working.
func TestLabelsThatOnlyDrawAreStillPublished(t *testing.T) {
	for name, label := range map[string]string{
		"a plain diagram": "a -> b\n",
		"bold text":       "a: |md\n  **bold** and _italic_\n|\n",
		"a link":          "a: |md\n  [a link](https://example.com)\n|\n",
		"a remote image":  "a: |md\n  <img src=\"https://example.com/a.png\" />\n|\n",
		"a list and code": "a: |md\n  - one\n  - `two`\n|\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ProcessD2SVG("probe", []byte(label), "", 1.0, false)
			require.NoError(t, err)
			assert.NotEmpty(t, got.FileBytes)
		})
	}
}

// TestBundledImagesSurviveTheCheck pins the one data: URL that is not a way to
// run something: the bundler inlines pictures as data:image/..., and refusing
// those would refuse the feature that puts them there.
func TestBundledImagesSurviveTheCheck(t *testing.T) {
	require.NoError(t, checkDrawingIsSafe([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AAAA"/></svg>`)))

	require.Error(t, checkDrawingIsSafe([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg"><a href="data:text/html,x">t</a></svg>`)))
}
