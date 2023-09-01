package mark

import (
	"bytes"
	"regexp"

	"github.com/kovetskiy/mark/pkg/mark/attachment"
	cparser "github.com/kovetskiy/mark/pkg/mark/parser"
	crenderer "github.com/kovetskiy/mark/pkg/mark/renderer"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/pkg/log"
	"github.com/yuin/goldmark"

	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// Renderer renders anchor [Node]s.
type ConfluenceExtension struct {
	html.Config
	Stdlib          *stdlib.Lib
	Path            string
	MermaidProvider string
	MermaidScale    float64
	DropFirstH1     bool
	Attachments     []attachment.Attachment
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceExtension(stdlib *stdlib.Lib, path string, mermaidProvider string, mermaidScale float64, dropFirstH1 bool) *ConfluenceExtension {
	return &ConfluenceExtension{
		Config:          html.NewConfig(),
		Stdlib:          stdlib,
		Path:            path,
		MermaidProvider: mermaidProvider,
		MermaidScale:    mermaidScale,
		DropFirstH1:     dropFirstH1,
		Attachments:     []attachment.Attachment{},
	}
}

func (c *ConfluenceExtension) Extend(m goldmark.Markdown) {

	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceBlockQuoteRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, &c.Attachments, c.MermaidProvider, c.MermaidScale), 100),
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, &c.Attachments, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
	))

	m.Parser().AddOptions(parser.WithInlineParsers(
		// Must be registered with a higher priority than goldmark's linkParser to make sure goldmark doesn't parse
		// the <ac:*/> tags.
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, mermaidProvider string, mermaidScale float64, dropFirstH1 bool) (string, []attachment.Attachment) {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	confluenceExtension := NewConfluenceExtension(stdlib, path, mermaidProvider, mermaidScale, dropFirstH1)

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
			confluenceExtension,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
		))

	var buf bytes.Buffer
	err := converter.Convert(markdown, &buf)

	if err != nil {
		panic(err)
	}

	html := buf.Bytes()

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html), confluenceExtension.Attachments

}

// ExtractDocumentLeadingH1 will extract leading H1 heading
func ExtractDocumentLeadingH1(markdown []byte) string {
	h1 := regexp.MustCompile(`#[^#]\s*(.*)\s*\n`)
	groups := h1.FindSubmatch(markdown)
	if groups == nil {
		return ""
	} else {
		return string(groups[1])
	}
}
