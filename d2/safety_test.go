package d2

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiagramCannotRunSomethingOnTheMachinePublishing is the reason for all of
// this.
//
// The PNG is taken by navigating a browser to the drawing as a document -- so
// anything the drawing runs, runs on the machine doing the publishing, in a
// browser started with --no-sandbox. A diagram in a pull request could read
// anything the runner can reach and send it somewhere.
//
// The listener asserts the negative that matters, whichever layer is what
// stops it: not merely that the run failed, but that nothing was ever
// requested.
func TestDiagramCannotRunSomethingOnTheMachinePublishing(t *testing.T) {
	var requested atomic.Bool

	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(true)
	}))
	defer listener.Close()

	diagram := []byte("a: |md\n  <script>setTimeout(function(){location.href=\"" +
		listener.URL + "/exfil\"},500)</script>\n|\n")

	for _, drawing := range publishable(t, diagram) {
		assert.NotContains(t, string(drawing), "<script")
		assert.NotContains(t, string(drawing), listener.URL)
	}

	// Longer than the script waits, so that a failure to block it would show.
	time.Sleep(2 * time.Second)

	assert.False(t, requested.Load(), "nothing should have been requested")
}

// TestUnsafeDiagramsAreRefused covers the rest of what a label can carry.
//
// d2 draws a markdown label as SVG of its own rather than passing the author's
// markup through, so most of these it turns away while compiling and the rest
// it draws as the text they are. What this pins is the property, not which
// layer holds it: none of it reaches a file that a browser is going to open.
func TestUnsafeDiagramsAreRefused(t *testing.T) {
	for name, unsafe := range map[string]struct{ label, marker string }{
		"an iframe":        {"<iframe src=\"https://example.com\"></iframe>", "<iframe"},
		"an object":        {"<object data=\"https://example.com\"></object>", "<object"},
		"an embed":         {"<embed src=\"https://example.com\" />", "<embed"},
		"an error handler": {"<img src=\"x.png\" onerror=\"alert(1)\" />", "onerror"},
		"a load handler":   {"<svg onload=\"alert(1)\"></svg>", "onload"},
		"a javascript url": {"<a href=\"javascript:alert(1)\">click</a>", "javascript:"},
	} {
		t.Run(name, func(t *testing.T) {
			diagram := []byte("a: |md\n  " + unsafe.label + "\n|\n")

			for _, drawing := range publishable(t, diagram) {
				assert.NotContains(t, string(drawing), unsafe.marker)
			}
		})
	}
}

// TestUnsafeLinksAreRefused covers what a diagram can still say for itself.
//
// A d2 link is not markup that d2 draws: it is an address that goes into the
// drawing as the anchor it was written as, so an address that is code rather
// than a page is mark's to refuse.
func TestUnsafeLinksAreRefused(t *testing.T) {
	for name, link := range map[string]string{
		"a javascript url": "javascript:alert(1)",
		"a vbscript url":   "vbscript:msgbox(1)",
		"a data document":  "\"data:text/html,<h1>x</h1>\"",
	} {
		t.Run(name, func(t *testing.T) {
			diagram := []byte("a: click me\na.link: " + link + "\n")

			_, err := ProcessD2SVG("probe", diagram, "", 1.0, false)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnsafeDiagram)

			_, err = ProcessD2("probe", diagram, 1.0)
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

// publishable renders a diagram both ways and returns the drawings that would
// be uploaded or opened.
//
// A diagram that is refused contributes nothing: whether d2 declines to draw it
// or mark declines to publish it, it is a diagram that no browser is going to
// see, which is the whole of what the tests above are asking about.
func publishable(t *testing.T, diagram []byte) [][]byte {
	t.Helper()

	var drawings [][]byte

	if png, err := ProcessD2("png", diagram, 1.0); err == nil {
		drawings = append(drawings, png.FileBytes)
	}

	if svg, err := ProcessD2SVG("svg", diagram, "", 1.0, false); err == nil {
		require.True(t, strings.Contains(string(svg.FileBytes), "<svg"),
			"a diagram that was published should be a drawing")

		drawings = append(drawings, svg.FileBytes)
	}

	return drawings
}
