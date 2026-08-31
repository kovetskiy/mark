package renderer_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func paragraphRenderers() []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextLegacyRenderer(false),
	}
}

// TestParagraphWrapsProse is the ordinary case.
func TestParagraphWrapsProse(t *testing.T) {
	actual := render(t, "Some prose.\n", paragraphRenderers())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<p>Some prose.</p>")
}

// TestParagraphOpeningWithRawHTMLIsNotWrapped covers why this renderer exists
// at all.
//
// A line that starts with a raw tag is a macro the author wrote by hand --
// <ac:structured-macro>, <ac:link>, an <ac:image> -- and several of those are
// not valid inside a <p>. Wrapping them produces storage format the editor
// normalises on the next save, moving or dropping the macro, so a paragraph
// whose first child is raw HTML is written without the wrapper.
func TestParagraphOpeningWithRawHTMLIsNotWrapped(t *testing.T) {
	actual := render(t, "<ac:structured-macro ac:name=\"toc\"/>\n",
		paragraphRenderers(),
		parser.WithInlineParsers(util.Prioritized(cparser.NewConfluenceTagParser(), 199)))

	assert.NotContains(t, actual, "<p>", "the macro must not be wrapped")
	assert.Contains(t, actual, `<ac:structured-macro ac:name="toc"/>`)
}

// TestParagraphWithRawHTMLLaterIsStillWrapped is the boundary of that rule:
// only a paragraph that *opens* with a tag skips the wrapper, so prose with a
// macro in the middle keeps its <p> and stays a paragraph.
func TestParagraphWithRawHTMLLaterIsStillWrapped(t *testing.T) {
	actual := render(t, "text <ac:structured-macro ac:name=\"toc\"/>\n",
		paragraphRenderers(),
		parser.WithInlineParsers(util.Prioritized(cparser.NewConfluenceTagParser(), 199)))

	assert.Contains(t, actual, "<p>text ")
	assert.Contains(t, actual, "</p>")
}
