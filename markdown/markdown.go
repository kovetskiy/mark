package mark

import (
	"bytes"
	"slices"

	"github.com/kovetskiy/mark/attachment"
	cparser "github.com/kovetskiy/mark/parser"
	crenderer "github.com/kovetskiy/mark/renderer"
	"github.com/kovetskiy/mark/stdlib"
	ctransformer "github.com/kovetskiy/mark/transformer"
	"github.com/kovetskiy/mark/types"
	"github.com/reconquest/pkg/log"
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
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.MarkConfig.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceParagraphRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
	))

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

	m.Parser().AddOptions(parser.WithInlineParsers(
		// Must be registered with a higher priority than goldmark's linkParser to make sure goldmark doesn't parse
		// the <ac:*/> tags.
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

// compileMarkdownWithExtension is a shared helper to eliminate code duplication
// between different compilation approaches
func compileMarkdownWithExtension(markdown []byte, ext goldmark.Extender, logMessage string) (string, []attachment.Attachment) {
	log.Tracef(nil, logMessage, string(markdown))

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

	// We'll return attachments separately - caller handles this
	return string(html), []attachment.Attachment{}
}

// CompileMarkdown compiles markdown to Confluence Storage Format with GitHub Alerts support
// This is the main function that now uses the enhanced GitHub Alerts transformer by default
// for superior processing of [!NOTE], [!TIP], [!WARNING], [!CAUTION], [!IMPORTANT] syntax
func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment) {
	// Use the enhanced GitHub Alerts extension for better processing
	ghAlertsExtension := NewConfluenceExtension(stdlib, path, cfg)
	html, _ := compileMarkdownWithExtension(markdown, ghAlertsExtension, "rendering markdown with GitHub Alerts support:\n%s")
	return html, ghAlertsExtension.Attachments
}

// CompileMarkdownLegacy compiles markdown using the legacy approach without GitHub Alerts transformer
// This function is preserved for backward compatibility and testing purposes
func CompileMarkdownLegacy(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment) {
	confluenceExtension := NewConfluenceLegacyExtension(stdlib, path, cfg)
	html, _ := compileMarkdownWithExtension(markdown, confluenceExtension, "rendering markdown with legacy renderer:\n%s")
	return html, confluenceExtension.Attachments
}

// ConfluenceExtension is a goldmark extension for GitHub Alerts with Transformer approach
// This extension provides superior GitHub Alert processing by transforming [!NOTE], [!TIP], etc.
// into proper Confluence macros while maintaining full compatibility with existing functionality.
// This is now the primary/default extension.
type ConfluenceExtension struct {
	html.Config
	Stdlib      *stdlib.Lib
	Path        string
	MarkConfig  types.MarkConfig
	Attachments []attachment.Attachment
}

// NewConfluenceExtension creates a new instance of the GitHub Alerts extension
// This is the improved standalone version that doesn't depend on feature flags
func NewConfluenceExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig) *ConfluenceExtension {
	return &ConfluenceExtension{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		MarkConfig:  cfg,
		Attachments: []attachment.Attachment{},
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
		util.Prioritized(crenderer.NewConfluenceHTMLBlockRenderer(c.Stdlib), 100),
		util.Prioritized(crenderer.NewConfluenceHeadingRenderer(c.MarkConfig.DropFirstH1), 100),
		util.Prioritized(crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path), 100),
		util.Prioritized(crenderer.NewConfluenceParagraphRenderer(), 100),
		util.Prioritized(crenderer.NewConfluenceLinkRenderer(), 100),
	))

	// Add GitHub Alerts specific renderers with higher priority to override defaults
	// These renderers handle both GitHub Alerts and legacy blockquote syntax
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(), 200),
		util.Prioritized(crenderer.NewConfluenceTextRenderer(c.MarkConfig.StripNewlines), 200),
	))

	// Add the GitHub Alerts AST transformer that preprocesses [!TYPE] syntax
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewGHAlertsTransformer(), 100),
	))

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

	// Add confluence tag parser for <ac:*/> tags
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))
}

// CompileMarkdownWithTransformer compiles markdown using the transformer approach for GitHub Alerts
// This function provides enhanced GitHub Alert processing while maintaining full compatibility
// with existing markdown functionality. It transforms [!NOTE], [!TIP], etc. into proper titles.
// This is an alias for CompileMarkdown for backward compatibility.
func CompileMarkdownWithTransformer(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment) {
	return CompileMarkdown(markdown, stdlib, path, cfg)
}

// Approach 2: Decorator Pattern Implementation
// CompileMarkdownDecorator wraps the compilation process with configurable GitHub Alerts support

// MarkdownCompiler interface defines the contract for markdown compilation
type MarkdownCompiler interface {
	Compile(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment)
}

// LegacyMarkdownCompiler implements the original compilation without GitHub Alerts transformer
type LegacyMarkdownCompiler struct{}

func (c *LegacyMarkdownCompiler) Compile(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment) {
	return CompileMarkdownLegacy(markdown, stdlib, path, cfg)
}

// GHAlertsMarkdownCompiler implements compilation with GitHub Alerts transformer
type GHAlertsMarkdownCompiler struct{}

func (c *GHAlertsMarkdownCompiler) Compile(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment) {
	return CompileMarkdownWithTransformer(markdown, stdlib, path, cfg)
}

// CompileMarkdownWithDecorator allows choosing between legacy and GitHub Alerts approaches
// This provides a flexible way to switch implementations based on configuration or feature flags
func CompileMarkdownWithDecorator(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig, useGHAlerts bool) (string, []attachment.Attachment) {
	var compiler MarkdownCompiler

	if useGHAlerts {
		compiler = &GHAlertsMarkdownCompiler{}
		log.Tracef(nil, "using GitHub Alerts transformer compiler")
	} else {
		compiler = &LegacyMarkdownCompiler{}
		log.Tracef(nil, "using legacy compiler")
	}

	return compiler.Compile(markdown, stdlib, path, cfg)
}
