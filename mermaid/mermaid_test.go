package mermaid

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMermaidImage(t *testing.T) {
	tests := []struct {
		name     string
		markdown []byte
		scale    float64
		want     attachment.Attachment
		wantErr  assert.ErrorAssertionFunc
	}{
		{"example", []byte("graph TD;\n A-->B;"), 1.0, attachment.Attachment{
			// This is only the PNG Magic Header
			FileBytes: []byte{0x89, 0x50, 0x4e, 0x47, 0xd, 0xa, 0x1a, 0xa},
			Filename:  "example.png",
			Name:      "example",
			Replace:   "example",
			Checksum:  "26296b73c960c25850b37bc9dd77cb24fce1a78db83b37755a25af7f8a48cc96",
			ID:        "",
		},
			assert.NoError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessMermaidLocally(tt.name, tt.markdown, tt.scale)
			if !tt.wantErr(t, err, fmt.Sprintf("processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))) {
				return
			}
			assert.Equal(t, tt.want.Filename, got.Filename, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			// We only test for the header as png changes based on system png library
			assert.Equal(t, tt.want.FileBytes, got.FileBytes[0:8], "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Name, got.Name, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Replace, got.Replace, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Checksum, got.Checksum, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.ID, got.ID, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			gotWidth, widthErr := strconv.ParseInt(got.Width, 10, 64)
			assert.NoError(t, widthErr, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotWidth, int64(0), "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))

			gotHeight, heightErr := strconv.ParseInt(got.Height, 10, 64)
			assert.NoError(t, heightErr, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotHeight, int64(0), "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}

// A diagram mermaid.js refuses is reported as ErrRenderException, which says the
// page raised an exception rather than that the browser died. The engine is
// therefore kept: before mermaid.go classified its failures, every bad diagram
// in a run tore Chrome down and made the next one pay a relaunch.
func TestInvalidDiagramKeepsEngine(t *testing.T) {
	before, err := getMermaidEngine()
	require.NoError(t, err)

	_, err = ProcessMermaidLocally("invalid", []byte("this is not a mermaid diagram"), 1.0)
	require.Error(t, err)
	assert.ErrorIs(t, err, mermaid.ErrRenderException)

	after, err := getMermaidEngine()
	require.NoError(t, err)
	assert.Same(t, before, after)
}

// A render that outruns renderTimeout also leaves the engine usable, because
// cancelling it aborts only the commands in flight. Both timeouts are shortened
// here: the context one bounds the wait for a turn on the page, the engine one
// bounds the render itself.
func TestRenderTimeoutKeepsEngine(t *testing.T) {
	before, err := getMermaidEngine()
	require.NoError(t, err)

	original := renderTimeout
	t.Cleanup(func() {
		renderTimeout = original
		before.SetRenderTimeout(original)
	})
	renderTimeout = time.Nanosecond
	before.SetRenderTimeout(time.Nanosecond)

	_, err = ProcessMermaidLocally("timeout", []byte("graph TD;\n A-->B;"), 1.0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	renderTimeout = original
	before.SetRenderTimeout(original)

	after, err := getMermaidEngine()
	require.NoError(t, err)
	assert.Same(t, before, after)

	got, err := ProcessMermaidLocally("after-timeout", []byte("graph TD;\n A-->B;"), 1.0)
	require.NoError(t, err)
	assert.NotEmpty(t, got.FileBytes)
}

// TestProcessMermaidSVG covers the other thing a diagram can be published as:
// the drawing itself rather than a picture of it.
func TestProcessMermaidSVG(t *testing.T) {
	got, err := ProcessMermaidSVG("svgtest", []byte("graph TD;\n A-->B;"), 1.0)
	require.NoError(t, err)

	assert.Equal(t, "svgtest.svg", got.Filename)
	assert.Equal(t, "svgtest", got.Name)
	assert.Equal(t, "svgtest", got.Replace)
	assert.True(t, hasSVGRoot(string(got.FileBytes)), "the attachment should be an SVG document")

	assertPixelSize(t, got.Width, got.Height)
}

// TestProcessMermaidWithBundle covers --mermaid-bundle: the same SVG, with the
// diagram's own source kept in it, so that what was published can be opened and
// edited again without the document it came from.
func TestProcessMermaidWithBundle(t *testing.T) {
	got, err := ProcessMermaidWithBundle("bundletest", []byte("graph TD;\n A-->B;"), 1.0)
	require.NoError(t, err)

	assert.Equal(t, "bundletest.svg", got.Filename)
	assert.True(t, hasSVGRoot(string(got.FileBytes)), "the attachment should be an SVG document")

	source, found := descContent(string(got.FileBytes))
	require.True(t, found, "a bundled SVG carries its source in a <desc> element")
	assert.Contains(t, source, "graph TD;")

	assertPixelSize(t, got.Width, got.Height)
}

// TestSVGChecksumsDifferByBundle covers what decides whether a diagram already
// on the page is uploaded again. The same diagram with and without its source
// is two different attachments, so a checksum that could not tell them apart
// would leave the first one in place forever.
func TestSVGChecksumsDifferByBundle(t *testing.T) {
	diagram := []byte("graph TD;\n A-->B;")

	plain, err := ProcessMermaidSVG("chk", diagram, 1.0)
	require.NoError(t, err)

	bundled, err := ProcessMermaidWithBundle("chk", diagram, 1.0)
	require.NoError(t, err)

	assert.NotEqual(t, plain.Checksum, bundled.Checksum)
}

// TestExtractSVGDimensions covers the sizes an SVG can state, and the one it
// cannot: a diagram drawn as width="100%" states no number of pixels at all.
// Read as 100 of them, a wide diagram is laid out as a narrow one, so anything
// that is not an absolute length falls back to the viewBox. mermaid is asked
// not to draw that way any more, but a diagram rendered before that, or by
// anything else, still may.
func TestExtractSVGDimensions(t *testing.T) {
	tests := []struct {
		name   string
		svg    string
		width  float64
		height float64
	}{
		{
			name:   "width and height attributes",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="200"></svg>`,
			width:  300,
			height: 200,
		},
		{
			name:   "px is a number of pixels like any other",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="450px" height="300px"></svg>`,
			width:  450,
			height: 300,
		},
		{
			// Reported as stated. Rounding happens once, after the scale
			// has been applied, since rounding first would scale a number
			// that had already lost its fraction.
			name:   "a fraction is not rounded here",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="85.4375" height="42.5"></svg>`,
			width:  85.4375,
			height: 42.5,
		},
		{
			name:   "no width or height at all falls back to the viewBox",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 480"></svg>`,
			width:  640,
			height: 480,
		},
		{
			name:   "and so does whichever of the two is missing",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="320" viewBox="0 0 640 480"></svg>`,
			width:  320,
			height: 480,
		},
		{
			name:   "a percentage is not a size",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 800 600"></svg>`,
			width:  800,
			height: 600,
		},
		{
			name:   "nor is a length in em",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="20em" height="15em" viewBox="0 0 320 240"></svg>`,
			width:  320,
			height: 240,
		},
		{
			name:   "the viewBox may separate its numbers with commas",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0,0,640,480"></svg>`,
			width:  640,
			height: 480,
		},
		{
			name:   "nothing to go on is reported as nothing",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			width:  0,
			height: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := extractSVGDimensions(test.svg)

			assert.InDelta(t, test.width, width, 0.0001)
			assert.InDelta(t, test.height, height, 0.0001)
		})
	}
}

// assertPixelSize asserts that a rendered diagram reported a size the page can
// lay out: whole pixels, and more than none of them.
func assertPixelSize(t *testing.T, width, height string) {
	t.Helper()

	pixels, err := strconv.Atoi(width)
	require.NoError(t, err, "width should be a whole number of pixels")
	assert.Positive(t, pixels)

	pixels, err = strconv.Atoi(height)
	require.NoError(t, err, "height should be a whole number of pixels")
	assert.Positive(t, pixels)
}

// hasSVGRoot reports whether the document's root element is an <svg>, looking
// past a processing instruction or a comment before it.
func hasSVGRoot(document string) bool {
	decoder := xml.NewDecoder(strings.NewReader(document))

	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}

		if element, ok := token.(xml.StartElement); ok {
			return element.Name.Local == "svg"
		}
	}
}

// descContent returns the text of the first <desc> element, whatever namespace
// or attributes it carries.
func descContent(document string) (string, bool) {
	decoder := xml.NewDecoder(strings.NewReader(document))

	for {
		token, err := decoder.Token()
		if err != nil {
			return "", false
		}

		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "desc" {
			continue
		}

		var text strings.Builder

		for {
			inner, err := decoder.Token()
			if err != nil {
				break
			}

			if data, ok := inner.(xml.CharData); ok {
				text.Write(data)
			}

			if end, ok := inner.(xml.EndElement); ok && end.Name.Local == "desc" {
				break
			}
		}

		return text.String(), true
	}
}

// TestSVGStatesItsOwnSizeUncapped covers what mermaid is asked to stop doing.
//
// Left alone it draws a diagram as width="100%" under a max-width style, which
// is a page's answer to a diagram wider than its column. These are not shown on
// a page: they are uploaded and displayed at the size mark asks for, which the
// cap then overrides -- and the file states no size of its own for mark to ask
// with.
func TestSVGStatesItsOwnSizeUncapped(t *testing.T) {
	got, err := ProcessMermaidSVG("uncapped", []byte("graph TD;\n A-->B;"), 1.0)
	require.NoError(t, err)

	// The root element, not the whole file: mermaid ships a stylesheet inside
	// the drawing, and a max-width in there styles something the diagram
	// contains rather than capping the diagram itself.
	root := svgRootElement(t, got.FileBytes)
	assert.NotContains(t, root, "max-width",
		"the drawing should carry no cap on how wide it may be shown")
	assert.NotContains(t, root, `width="100%"`,
		"and should state a width of its own instead")
}

// TestSVGIsUncappedForEveryKindOfDiagram is what keeps that true. useMaxWidth is
// set for each kind of diagram separately, so the configuration is built from
// mermaid's own defaults rather than a list written here -- and this fails if a
// mermaid release ever puts a diagram beyond its reach.
func TestSVGIsUncappedForEveryKindOfDiagram(t *testing.T) {
	diagrams := map[string]string{
		"flowchart": "graph TD;\n A-->B;",
		"sequence":  "sequenceDiagram\n Alice->>Bob: Hi",
		"pie":       "pie\n \"a\" : 40\n \"b\" : 60",
		"state":     "stateDiagram-v2\n [*] --> Still",
		"gantt":     "gantt\n title T\n section S\n A task :a1, 2024-01-01, 30d",
	}

	for name, source := range diagrams {
		t.Run(name, func(t *testing.T) {
			got, err := ProcessMermaidSVG(name, []byte(source), 1.0)
			require.NoError(t, err)

			assert.NotContains(t, svgRootElement(t, got.FileBytes), "max-width")
			assertPixelSize(t, got.Width, got.Height)
		})
	}
}

// TestSVGScaleMultipliesWhatThePageShowsAndNotTheFile covers what the scale
// means for a drawing that is the same at every size: the page shows it larger,
// the file does not change, and neither does the checksum that decides whether
// it is uploaded again.
func TestSVGScaleMultipliesWhatThePageShowsAndNotTheFile(t *testing.T) {
	diagram := []byte("graph TD;\n A-->B;")

	plain, err := ProcessMermaidSVG("scaled", diagram, 1.0)
	require.NoError(t, err)

	scaled, err := ProcessMermaidSVG("scaled", diagram, 2.0)
	require.NoError(t, err)

	assert.Equal(t, plain.Checksum, scaled.Checksum, "the drawing did not change")
	assert.Equal(t, plain.FileBytes, scaled.FileBytes)

	plainWidth, err := strconv.Atoi(plain.Width)
	require.NoError(t, err)

	scaledWidth, err := strconv.Atoi(scaled.Width)
	require.NoError(t, err)

	// Within a pixel of double, and not exactly double, because the drawing is
	// 85.4375 wide: the scale is applied to that and rounded once, giving 171
	// rather than the 170 that rounding first would have given.
	assert.InDelta(t, plainWidth*2, scaledWidth, 1,
		"the page shows it twice as wide")
	assert.Greater(t, scaledWidth, plainWidth)
}

// TestPNGIsUnaffectedByTheUncapping is the safety condition on all of this. Both
// outputs share one engine and one configuration, and the PNG is a screenshot of
// the same drawing: whatever the cap is worth for an SVG, it must not move a
// picture that people are already publishing.
func TestPNGIsUnaffectedByTheUncapping(t *testing.T) {
	got, err := ProcessMermaidLocally("png", []byte("graph TD;\n A-->B;"), 1.0)
	require.NoError(t, err)

	assert.Equal(t, "85", got.Width, "the picture is the size it has always been")
	assert.Equal(t, "174", got.Height)
}

// svgRootElement returns the opening <svg ...> tag, which is where a drawing
// states how large it is and how large it may be shown.
func svgRootElement(t *testing.T, document []byte) string {
	t.Helper()

	decoder := xml.NewDecoder(bytes.NewReader(document))

	for {
		token, err := decoder.Token()
		require.NoError(t, err, "the attachment should be a well-formed SVG")

		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		require.Equal(t, "svg", element.Name.Local, "the root element should be an svg")

		var rendered strings.Builder
		for _, attr := range element.Attr {
			fmt.Fprintf(&rendered, " %s=%q", attr.Name.Local, attr.Value)
		}

		return rendered.String()
	}
}
