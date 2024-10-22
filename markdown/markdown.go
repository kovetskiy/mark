package mark

import (
	"bytes"

	"github.com/kovetskiy/mark/attachment"
	cparser "github.com/kovetskiy/mark/parser"
	crenderer "github.com/kovetskiy/mark/renderer"
	"github.com/kovetskiy/mark/stdlib"
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
	StripNewlines   bool
	Attachments     []attachment.Attachment
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceExtension(stdlib *stdlib.Lib, path string, mermaidProvider string, mermaidScale float64, dropFirstH1 bool, stripNewlines bool) *ConfluenceExtension {
	return &ConfluenceExtension{
		Config:          html.NewConfig(),
		Stdlib:          stdlib,
		Path:            path,
		MermaidProvider: mermaidProvider,
		MermaidScale:    mermaidScale,
		DropFirstH1:     dropFirstH1,
		StripNewlines:   stripNewlines,
		Attachments:     []attachment.Attachment{},
	}
}

func (c *ConfluenceExtension) Attach(a attachment.Attachment) {
	c.Attachments = append(c.Attachments, a)
}

func (c *ConfluenceExtension) Extend(m goldmark.Markdown) {

	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceTextRenderer(c.StripNewlines), 100),
		util.Prioritized(crenderer.NewConfluenceBlockQuoteRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, c, c.MermaidProvider, c.MermaidScale), 100),
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceParagraphRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
	))

	m.Parser().AddOptions(parser.WithInlineParsers(
		// Must be registered with a higher priority than goldmark's linkParser to make sure goldmark doesn't parse
		// the <ac:*/> tags.
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, mermaidProvider string, mermaidScale float64, dropFirstH1 bool, stripNewlines bool) (string, []attachment.Attachment) {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	confluenceExtension := NewConfluenceExtension(stdlib, path, mermaidProvider, mermaidScale, dropFirstH1, stripNewlines)

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
			),
			confluenceExtension,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			html.WithXHTML(),
		))

	ctx := parser.NewContext(parser.WithIDs(&cparser.ConfluenceIDs{Values: map[string]bool{}}))

	var buf bytes.Buffer
	err := converter.Convert(markdown, &buf, parser.WithContext(ctx))

	if err != nil {
		panic(err)
	}

	html := buf.Bytes()

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html), confluenceExtension.Attachments
}
