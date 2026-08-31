package renderer_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/renderer"
)

// imageRenderers renders images as if the document were testdata/doc.md, which
// is what makes "test.png" beside it resolvable as a local attachment.
func imageRenderers(t *testing.T, attacher attachment.Attacher) []renderer.NodeRenderer {
	t.Helper()

	return []renderer.NodeRenderer{
		crenderer.NewConfluenceImageRenderer(newStdlib(t), attacher, "../testdata/doc.md", ""),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextRenderer(false),
	}
}

// TestImageFromURL covers an image mark cannot upload: a remote one stays a
// remote one, referenced by ri:url rather than uploaded and referenced by name.
func TestImageFromURL(t *testing.T) {
	actual := render(t, "![alt](https://example.com/a.png \"Title\")\n", imageRenderers(t, &collectingAttacher{}))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:image ac:title="Title" ac:alt="alt"><ri:url ri:value="https://example.com/a.png"/></ac:image>`)
}

// TestImageURLAmpersandIsEscaped covers the query string, which is where a URL
// most often carries an "&" -- unescaped, it makes the whole body malformed and
// Confluence rejects the page rather than the image.
func TestImageURLAmpersandIsEscaped(t *testing.T) {
	actual := render(t, "![alt](https://example.com/a.png?x=1&y=2)\n", imageRenderers(t, &collectingAttacher{}))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ri:value="https://example.com/a.png?x=1&amp;y=2"`)
}

// TestImageAltIsEscaped covers alt text carrying markup characters. The same
// reasoning as the URL: it is an attribute value.
func TestImageAltIsEscaped(t *testing.T) {
	actual := render(t, "![a & b](https://example.com/a.png)\n", imageRenderers(t, &collectingAttacher{}))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:alt="a &amp; b"`)
}

// TestImageFromLocalFileBecomesAnAttachment covers the other half: a path that
// resolves beside the document is uploaded, referenced by filename, and its
// real pixel size is published so that Confluence lays the page out without
// having to load the image first.
func TestImageFromLocalFileBecomesAnAttachment(t *testing.T) {
	attacher := &collectingAttacher{}

	actual := render(t, "![local](test.png)\n", imageRenderers(t, attacher))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ri:attachment ri:filename="test.png"/>`)
	assert.Contains(t, actual, `ac:original-width="1000"`)
	assert.Contains(t, actual, `ac:original-height="631"`)

	require.Len(t, attacher.attachments, 1, "the file has to be queued for upload, or the page renders a broken image")
	assert.Equal(t, "test.png", attacher.attachments[0].Filename)
}

// TestImageWithUnresolvablePathIsTreatedAsAURL records what happens to a local
// path that does not exist: it is not an error, it falls through to the URL
// branch and is published as a relative ri:url. A typo in an image path
// therefore reaches the page as a broken image rather than stopping the run.
func TestImageWithUnresolvablePathIsTreatedAsAURL(t *testing.T) {
	attacher := &collectingAttacher{}

	actual := render(t, "![missing](no-such-file.png)\n", imageRenderers(t, attacher))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ri:url ri:value="no-such-file.png"/>`)
	assert.Empty(t, attacher.attachments)
}
