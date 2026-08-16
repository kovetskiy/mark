package mark

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"text/template"

	katex "github.com/FurqanSoftware/goldmark-katex"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/includes"
	"github.com/kovetskiy/mark/v16/macro"
	cparser "github.com/kovetskiy/mark/v16/parser"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/stdlib"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/rs/zerolog/log"
	mkDocsParser "github.com/stefanfritsch/goldmark-admonitions"
	"github.com/yuin/goldmark"

	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// ConfluenceLegacyExtension is the original goldmark extension without GitHub Alerts support
// This extension is preserved for backward compatibility and testing purposes
type ConfluenceLegacyExtension struct {
	html.Config
	Stdlib      *stdlib.Lib
	Path        string
	MarkConfig  types.MarkConfig
	Attachments []attachment.Attachment
}

// NewConfluenceLegacyExtension creates a new instance of the legacy ConfluenceRenderer
func NewConfluenceLegacyExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig) *ConfluenceLegacyExtension {
	return &ConfluenceLegacyExtension{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		MarkConfig:  cfg,
		Attachments: []attachment.Attachment{},
	}
}

func (c *ConfluenceLegacyExtension) Attach(a attachment.Attachment) {
	c.Attachments = append(c.Attachments, a)
}

func (c *ConfluenceLegacyExtension) Extend(m goldmark.Markdown) {

	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceTextLegacyRenderer(c.MarkConfig.StripNewlines), 100),
		util.Prioritized(crenderer.NewConfluenceBlockQuoteRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, c, c.MarkConfig), 100),
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.MarkConfig.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign), 100),
		util.Prioritized(crenderer.NewConfluenceParagraphRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
	))

	if slices.Contains(c.MarkConfig.Features, "details") {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(ctransformer.NewDetailsTransformer(), 110),
		))
	}

	if slices.Contains(c.MarkConfig.Features, "mkdocsadmonitions") {
		m.Parser().AddOptions(
			parser.WithBlockParsers(
				util.Prioritized(mkDocsParser.NewAdmonitionParser(), 100),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceMkDocsAdmonitionRenderer(), 100),
		))
	}

	if slices.Contains(c.MarkConfig.Features, "date") {
		m.Parser().AddOptions(
			parser.WithInlineParsers(
				util.Prioritized(cparser.NewDateParser(), 99),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceDateRenderer(), 100),
		))
	}

	if slices.Contains(c.MarkConfig.Features, "mention") {
		m.Parser().AddOptions(
			parser.WithInlineParsers(
				util.Prioritized(cparser.NewMentionParser(), 99),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceMentionRenderer(c.Stdlib), 100),
		))
	}

	if slices.Contains(c.MarkConfig.Features, "inline-link-card") {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(ctransformer.NewAutoLinkTransformer(), 110),
		))
	}

	m.Parser().AddOptions(parser.WithInlineParsers(
		// Must be registered with a higher priority than goldmark's linkParser to make sure goldmark doesn't parse
		// the <ac:*/> tags.
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

// compileMarkdownWithExtension is a shared helper to eliminate code duplication
// between different compilation approaches
func compileMarkdownWithExtension(markdown []byte, ext goldmark.Extender, logMessage string) (string, error) {
	log.Trace().Msgf(logMessage, string(markdown))

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.Footnote,
			extension.DefinitionList,
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
			),
			ext,
			extension.GFM,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			// Lets an author name a heading's anchor themselves, with the
			// {#custom-id} syntax every other Markdown tool understands.
			// Without it the braces are not ignored but taken as heading text:
			// they render visibly in the title and are folded into the
			// generated id, so "## Title {#custom-id}" becomes a heading called
			// "Title {#custom-id}" with the id "Title-custom-id".
			//
			// goldmark parses attributes on every block element, but each
			// renderer emits them through its own filter -- HeadingAttributeFilter,
			// ParagraphAttributeFilter and so on -- so nothing but headings is
			// affected in practice.
			parser.WithAttribute(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			html.WithXHTML(),
		))

	ctx := parser.NewContext(parser.WithIDs(cparser.NewConfluenceIDs()))

	var buf bytes.Buffer
	err := converter.Convert(markdown, &buf, parser.WithContext(ctx))

	if err != nil {
		return "", err
	}

	html := buf.Bytes()
	log.Trace().Msgf("rendered markdown to html:\n%s", string(html))

	return string(html), nil
}

func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	var err error

	ghAlertsExtension := NewConfluenceExtension(stdlib, path, cfg)
	htmlOutput, err := compileMarkdownWithExtension(markdown, ghAlertsExtension, "rendering markdown with GitHub Alerts support:\n%s")
	// A transformer cannot return an error from Transform, so each one that can
	// fail keeps its failure for collection here.
	if err == nil && ghAlertsExtension.Pipeline != nil && ghAlertsExtension.Pipeline.GetError() != nil {
		err = ghAlertsExtension.Pipeline.GetError()
	}
	if err == nil && ghAlertsExtension.Links != nil && ghAlertsExtension.Links.GetError() != nil {
		err = ghAlertsExtension.Links.GetError()
	}
	if err != nil {
		return "", nil, err
	}
	return htmlOutput, ghAlertsExtension.Attachments, nil
}

// CompileMarkdownLegacy compiles markdown using the legacy approach without GitHub Alerts transformer
// This function is preserved for backward compatibility and testing purposes
func CompileMarkdownLegacy(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	var tmpl *template.Template
	if stdlib != nil {
		tmpl = stdlib.Templates
	} else {
		tmpl = template.New("stdlib")
	}

	var err error
	var recurse bool
	for {
		tmpl, markdown, recurse, err = includes.ProcessIncludes(
			filepath.Dir(path),
			cfg.IncludePath,
			markdown,
			tmpl,
		)
		if err != nil {
			return "", nil, fmt.Errorf("unable to process includes: %w", err)
		}
		if !recurse {
			break
		}
	}

	var macros []macro.Macro
	macros, markdown, err = macro.ExtractMacros(filepath.Dir(path), cfg.IncludePath, markdown, tmpl)
	if err != nil {
		return "", nil, fmt.Errorf("unable to extract macros: %w", err)
	}
	for _, m := range macros {
		markdown, err = m.Apply(markdown)
		if err != nil {
			return "", nil, fmt.Errorf("unable to apply macro %q: %w", m.Regexp.String(), err)
		}
	}

	confluenceExtension := NewConfluenceLegacyExtension(stdlib, path, cfg)
	htmlOutput, err := compileMarkdownWithExtension(markdown, confluenceExtension, "rendering markdown with legacy renderer:\n%s")
	if err != nil {
		return "", nil, err
	}
	return htmlOutput, confluenceExtension.Attachments, nil
}

// ConfluenceExtension is a goldmark extension for GitHub Alerts with Transformer approach
// This extension provides superior GitHub Alert processing by transforming [!NOTE], [!TIP], etc.
// into proper Confluence macros while maintaining full compatibility with existing functionality.
// This is now the primary/default extension.
type ConfluenceExtension struct {
	html.Config
	Stdlib          *stdlib.Lib
	Path            string
	MarkConfig      types.MarkConfig
	Attachments     []attachment.Attachment
	Pipeline        *ctransformer.PipelineTransformer
	Links           *ctransformer.LinkTransformer
	AttachmentLinks *ctransformer.AttachmentTransformer
}

// NewConfluenceExtension creates a new instance of the GitHub Alerts extension
// This is the improved standalone version that doesn't depend on feature flags
func NewConfluenceExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig) *ConfluenceExtension {
	var tmpl *template.Template
	if stdlib != nil {
		tmpl = stdlib.Templates
	}
	pipeline := ctransformer.NewPipelineTransformer(
		// The base directory is the file's directory, not the file: a relative
		// include is written relative to the document that asks for it.
		ctransformer.NewMacroTransformer(path, filepath.Dir(path), cfg.IncludePath, tmpl),
		ctransformer.NewIncludeTransformer(path, filepath.Dir(path), cfg.IncludePath, tmpl),
	)
	return &ConfluenceExtension{
		Config:          html.NewConfig(),
		Stdlib:          stdlib,
		Path:            path,
		MarkConfig:      cfg,
		Attachments:     []attachment.Attachment{},
		Pipeline:        pipeline,
		Links:           ctransformer.NewLinkTransformer(cfg.ResolveLink),
		AttachmentLinks: ctransformer.NewAttachmentTransformer(cfg.ResolveAttachment),
	}
}

func (c *ConfluenceExtension) Attach(a attachment.Attachment) {
	c.Attachments = append(c.Attachments, a)
}

// Extend extends the Goldmark processor with GitHub Alerts transformer and renderers
// This method registers all necessary components for GitHub Alert processing:
// 1. Core renderers for standard markdown elements
// 2. GitHub Alerts specific renderers (blockquote and text) with higher priority
// 3. GitHub Alerts AST transformer for preprocessing
func (c *ConfluenceExtension) Extend(m goldmark.Markdown) {
	// Register core renderers (excluding blockquote and text which we'll replace)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, c, c.MarkConfig), 100),
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.MarkConfig.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign), 100),
		util.Prioritized(crenderer.NewConfluenceParagraphRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
	))

	// Add GitHub Alerts specific renderers with higher priority to override defaults
	// These renderers handle both GitHub Alerts and legacy blockquote syntax
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(), 200),
		util.Prioritized(crenderer.NewConfluenceTextRenderer(c.MarkConfig.StripNewlines), 200),
	))

	// Add AST Transformers for Macros, Includes, Layouts, and GitHub Alerts
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(c.Pipeline, 10),
		util.Prioritized(ctransformer.NewLayoutTransformer(), 100),
		util.Prioritized(ctransformer.NewGHAlertsTransformer(), 100),
		// Last, so that it sees the headings includes and macros brought in as
		// well as the ones written in the file, and so that heading ids have
		// already been assigned.
		util.Prioritized(ctransformer.NewAnchorTransformer(), 900),
		// After includes and macros have brought their content in, so links
		// inside an included fragment are resolved too.
		// Before link resolution, so that a path a document declared as an
		// attachment is taken as one rather than looked up as a page.
		util.Prioritized(c.AttachmentLinks, 905),
		util.Prioritized(c.Links, 910),
	))

	// Add date widget support if requested
	if slices.Contains(c.MarkConfig.Features, "date") {
		m.Parser().AddOptions(
			parser.WithInlineParsers(
				util.Prioritized(cparser.NewDateParser(), 99),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceDateRenderer(), 100),
		))
	}

	// Add details transformer support if requested
	if slices.Contains(c.MarkConfig.Features, "details") {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(ctransformer.NewDetailsTransformer(), 110),
		))
	}

	// Add html-img-tag transformer support if requested
	if slices.Contains(c.MarkConfig.Features, "html-img-tag") {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(ctransformer.NewHTMLImgTransformer(), 110),
		))
	}

	// Add mkdocsadmonitions support if requested
	if slices.Contains(c.MarkConfig.Features, "mkdocsadmonitions") {
		m.Parser().AddOptions(
			parser.WithBlockParsers(
				util.Prioritized(mkDocsParser.NewAdmonitionParser(), 100),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceMkDocsAdmonitionRenderer(), 100),
		))
	}

	// Add mention support if requested
	if slices.Contains(c.MarkConfig.Features, "mention") {
		m.Parser().AddOptions(
			parser.WithInlineParsers(
				util.Prioritized(cparser.NewMentionParser(), 99),
			),
		)

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceMentionRenderer(c.Stdlib), 100),
		))
	}

	// Add math / latex formula support if requested via goldmark-katex
	if slices.Contains(c.MarkConfig.Features, "math") {
		(&katex.Extender{}).Extend(m)
	}

	// Add inline-link-card support if requested · renders auto-detected bare
	// URLs with the `data-card-appearance="inline"` hint, prompting Confluence
	// Cloud to display them as inline smart cards (page mentions, Jira issues,
	// GitHub references, etc.) instead of plain hyperlinks.
	if slices.Contains(c.MarkConfig.Features, "inline-link-card") {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(ctransformer.NewAutoLinkTransformer(), 110),
		))
	}
	// Add confluence tag parser for <ac:*/> tags
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

// CompileMarkdownWithTransformer compiles markdown using the transformer approach for GitHub Alerts
// This function provides enhanced GitHub Alert processing while maintaining full compatibility
// with existing markdown functionality. It transforms [!NOTE], [!TIP], etc. into proper titles.
// This is an alias for CompileMarkdown for backward compatibility.
func CompileMarkdownWithTransformer(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	return CompileMarkdown(markdown, stdlib, path, cfg)
}
