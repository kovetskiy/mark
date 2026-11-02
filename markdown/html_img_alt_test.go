package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The <img> transformer hands the alt text over as a plain ast.String, and the
// image renderer only ever read a String that was marked as code -- so the alt
// of an <img> was dropped, while the Markdown spelling of the same image kept
// its own. <img> is what authors reach for when they want a width.
func TestHTMLImgAltReachesTheImage(t *testing.T) {
	out := compileDoc(t,
		`<img src="https://example.com/a.png" alt="a diagram" width="100">`+"\n",
		"html-img-tag")

	assert.Contains(t, out, `ac:alt="a diagram"`)
}

// The alt is author text going into an XML attribute, so it is escaped on the
// way in exactly as the Markdown spelling's alt is.
func TestHTMLImgAltIsEscaped(t *testing.T) {
	out := compileDoc(t,
		`<img src="https://example.com/a.png" alt="a &amp; b">`+"\n",
		"html-img-tag")

	assert.Contains(t, out, `ac:alt="a &amp; b"`)
	assert.NotContains(t, out, "&amp;amp;")
}
