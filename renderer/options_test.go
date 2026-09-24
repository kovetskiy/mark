package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v17/renderer"
	"github.com/kovetskiy/mark/v17/types"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
)

// TestConstructorsHonourHTMLOptions covers the html options every constructor
// accepts. Only NewConfluenceHTMLBlockRenderer used to apply them; the rest
// took them and dropped them, so a caller building a renderer by hand and
// asking for, say, unsafe links or hard wraps got the defaults with nothing to
// say why.
//
// Checked on the Config rather than on rendered output: goldmark hands the
// options given to the converter to every renderer as well, so a render would
// pass whether the constructor applied them or not.
func TestConstructorsHonourHTMLOptions(t *testing.T) {
	lib := newStdlib(t)
	opts := []html.Option{html.WithUnsafe(), html.WithHardWraps()}

	constructors := map[string]renderer.NodeRenderer{
		"blockquote":        crenderer.NewConfluenceBlockQuoteRenderer(opts...),
		"codeblock":         crenderer.NewConfluenceCodeBlockRenderer(lib, opts...),
		"definitionlist":    crenderer.NewConfluenceDefinitionListRenderer(opts...),
		"fencedcodeblock":   crenderer.NewConfluenceFencedCodeBlockRenderer(lib, &collectingAttacher{}, types.MarkConfig{}, "", opts...),
		"gh alerts":         crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(opts...),
		"heading":           crenderer.NewConfluenceHeadingRenderer(lib, false, opts...),
		"htmlblock":         crenderer.NewConfluenceHTMLBlockRenderer(lib, &collectingAttacher{}, "", "", opts...),
		"image":             crenderer.NewConfluenceImageRenderer(lib, &collectingAttacher{}, "", "", opts...),
		"link":              crenderer.NewConfluenceLinkRenderer(lib, &collectingAttacher{}, "", false, opts...),
		"mkdocs admonition": crenderer.NewConfluenceMkDocsAdmonitionRenderer(opts...),
		"paragraph":         crenderer.NewConfluenceParagraphRenderer(opts...),
		"tasklist":          crenderer.NewConfluenceTaskListRenderer(opts...),
		"text":              crenderer.NewConfluenceTextRenderer(false, opts...),
	}

	for name, nodeRenderer := range constructors {
		t.Run(name, func(t *testing.T) {
			config := htmlConfig(t, nodeRenderer)

			assert.True(t, config.Unsafe, "WithUnsafe was dropped")
			assert.True(t, config.HardWraps, "WithHardWraps was dropped")
		})
	}
}

// htmlConfig digs out the html.Config each renderer embeds.
func htmlConfig(t *testing.T, nodeRenderer renderer.NodeRenderer) html.Config {
	t.Helper()

	switch r := nodeRenderer.(type) {
	case *crenderer.ConfluenceBlockQuoteRenderer:
		return r.Config
	case *crenderer.ConfluenceCodeBlockRenderer:
		return r.Config
	case *crenderer.ConfluenceDefinitionListRenderer:
		return r.Config
	case *crenderer.ConfluenceFencedCodeBlockRenderer:
		return r.Config
	case *crenderer.ConfluenceGHAlertsBlockQuoteRenderer:
		return r.Config
	case *crenderer.ConfluenceHeadingRenderer:
		return r.Config
	case *crenderer.ConfluenceHTMLBlockRenderer:
		return r.Config
	case *crenderer.ConfluenceImageRenderer:
		return r.Config
	case *crenderer.ConfluenceLinkRenderer:
		return r.Config
	case *crenderer.ConfluenceMkDocsAdmonitionRenderer:
		return r.Config
	case *crenderer.ConfluenceParagraphRenderer:
		return r.Config
	case *crenderer.ConfluenceTaskListRenderer:
		return r.Config
	case *crenderer.ConfluenceTextRenderer:
		return r.Config
	}

	t.Fatalf("%T is not a renderer this test knows", nodeRenderer)

	return html.Config{}
}
