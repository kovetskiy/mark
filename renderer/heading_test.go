package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
)

func headingRenderers(dropFirstH1 bool) []renderer.NodeRenderer {
	return []renderer.NodeRenderer{
		crenderer.NewConfluenceHeadingRenderer(dropFirstH1),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceTextLegacyRenderer(false),
	}
}

const headingDocument = "# First\n\ntext\n\n## Second\n\n# Third\n"

// TestHeadingDropFirstH1 covers --drop-h1, which exists because the page title
// usually repeats the document's first heading and Confluence shows the title
// above the body anyway.
//
// Only the first h1 goes. A later h1 is content, not a repeated title, and an
// h2 is never a title -- dropping either would silently lose a section heading.
func TestHeadingDropFirstH1(t *testing.T) {
	actual := render(t, headingDocument, headingRenderers(true))
	assertWellFormed(t, actual)

	assert.NotContains(t, actual, "First", "the first h1 goes, text and all")
	assert.Contains(t, actual, "<h2>Second</h2>")
	assert.Contains(t, actual, "<h1>Third</h1>")
}

// TestHeadingKeepsEverythingByDefault is the same document with the flag off.
func TestHeadingKeepsEverythingByDefault(t *testing.T) {
	actual := render(t, headingDocument, headingRenderers(false))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<h1>First</h1>")
	assert.Contains(t, actual, "<h2>Second</h2>")
	assert.Contains(t, actual, "<h1>Third</h1>")
}

// TestHeadingKeepsItsID covers the attribute a heading anchor is built from.
// mark links to headings by id, so a heading rendered without one is a link
// target that silently does not exist. How the id is spelled is the ID
// generator's business -- mark installs its own, which is what the
// heading-anchor fixtures in markdown/ pin -- and this only asserts that the
// renderer passes the attribute through.
func TestHeadingKeepsItsID(t *testing.T) {
	actual := render(t, "## A Section\n", headingRenderers(false), parser.WithAutoHeadingID())
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `id="a-section"`)
	assert.Contains(t, actual, "A Section</h2>")
}

// TestHeadingDropsOnlyTheFirstH1EvenWhenItIsNotFirstInTheDocument covers a
// document that opens with a paragraph: the flag drops the first h1 wherever it
// appears, rather than only when it is the opening block.
func TestHeadingDropsOnlyTheFirstH1EvenWhenItIsNotFirstInTheDocument(t *testing.T) {
	actual := render(t, "intro\n\n# Title\n\n# Later\n", headingRenderers(true))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "intro")
	assert.NotContains(t, actual, ">Title<")
	assert.Contains(t, actual, "<h1>Later</h1>")
}
