package mark

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"text/template"

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
	emoji "github.com/yuin/goldmark-emoji"

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

	// <details> reaches here only because the document wrote the tag, and the
	// storage format has no way to carry it, so leaving it alone publishes
	// markup Confluence discards or rejects. There is nothing to opt into.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewDetailsTransformer(), 110),
	))

	if slices.Contains(c.MarkConfig.Features, "emoji") {
		m.Parser().AddOptions(parser.WithInlineParsers(
			// The priority goldmark-emoji's own extension gives this parser.
			// Nothing else in the set triggers on ':'.
			util.Prioritized(emoji.NewParser(), 999),
		))

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			// Only the parser is taken from goldmark-emoji, so the renderer it
			// registers at 200 is not in play; 100 is what the rest of this
			// file uses.
			util.Prioritized(crenderer.NewConfluenceEmojiRenderer(c.Stdlib), 100),
		))
	}

	// goldmark's footnote extension is always on, so `[^1]` is always parsed.
	// The only question is what renders it, and the alternative is the HTML
	// that extension emits -- ids and fragment links, neither of which survives
	// a Confluence page.
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		// Below the 500 goldmark's own footnote extension registers at.
		// Node renderers are registered from the highest priority number
		// down, and each registration overwrites the last, so the *smaller*
		// number is the one that ends up rendering the node.
		util.Prioritized(crenderer.NewConfluenceFootnoteRenderer(c.Stdlib), 100),
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

	// Close the void elements and repair the comments an author may have
	// written by hand, which Markdown allows and storage format does not.
	// After everything at 110 that owns raw HTML of its own, so that this only
	// sees what those transformers left behind.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewXMLWellFormedTransformer(), 120),
	))

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
			// GFM's members are listed one by one rather than through
			// extension.GFM, which bundles its own default-configured Table.
			// Registering Table twice put two cell renderers at the same
			// priority 500, and goldmark sorts renderers with sort.Slice, which
			// is not stable -- so which alignment method survived was decided by
			// nothing at all. Alignment came out as ac:align attributes or as
			// style="text-align:..." depending on the sort.
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
			),
			ext,
			extension.Linkify,
			extension.Strikethrough,
			extension.TaskList,
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

// maxIncludePasses bounds the number of times the whole document is rescanned
// for include directives. ProcessIncludes drains every directive it can see in
// one pass, so a pass that still finds work is expansion feeding itself, and
// without a bound the document grows until the process is killed.
const maxIncludePasses = 10

// expandIncludes runs include expansion over the document until it settles,
// which is what both compile paths need before goldmark ever sees the bytes.
func expandIncludes(
	path string,
	includePath string,
	markdown []byte,
	tmpl *template.Template,
) (*template.Template, []byte, error) {
	for pass := 0; pass < maxIncludePasses; pass++ {
		var recurse bool
		var err error

		tmpl, markdown, recurse, err = includes.ProcessIncludes(
			filepath.Dir(path),
			includePath,
			markdown,
			tmpl,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to process includes: %w", err)
		}
		if !recurse {
			return tmpl, markdown, nil
		}
	}

	return nil, nil, fmt.Errorf("include expansion did not settle after %d passes over %q", maxIncludePasses, path)
}

func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	var tmpl *template.Template
	if stdlib != nil {
		tmpl = stdlib.Templates
	} else {
		tmpl = template.New("stdlib")
	}

	tmpl, markdown, err := expandIncludes(path, cfg.IncludePath, markdown, tmpl)
	if err != nil {
		return "", nil, err
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

	tmpl, markdown, err := expandIncludes(path, cfg.IncludePath, markdown, tmpl)
	if err != nil {
		return "", nil, err
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
		ctransformer.NewMacroTransformer(path, path, cfg.IncludePath, tmpl),
		ctransformer.NewIncludeTransformer(path, path, cfg.IncludePath, tmpl),
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

	// <details> reaches here only because the document wrote the tag, and the
	// storage format has no way to carry it, so leaving it alone publishes
	// markup Confluence discards or rejects. There is nothing to opt into.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewDetailsTransformer(), 110),
	))

	// Add emoji shortcode support if requested
	if slices.Contains(c.MarkConfig.Features, "emoji") {
		m.Parser().AddOptions(parser.WithInlineParsers(
			// The priority goldmark-emoji's own extension gives this parser.
			// Nothing else in the set triggers on ':'.
			util.Prioritized(emoji.NewParser(), 999),
		))

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			// Only the parser is taken from goldmark-emoji, so the renderer it
			// registers at 200 is not in play; 100 is what the rest of this
			// file uses.
			util.Prioritized(crenderer.NewConfluenceEmojiRenderer(c.Stdlib), 100),
		))
	}

	// goldmark's footnote extension is always on, so `[^1]` is always parsed.
	// The only question is what renders it, and the alternative is the HTML
	// that extension emits -- ids and fragment links, neither of which survives
	// a Confluence page.
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		// Below the 500 goldmark's own footnote extension registers at.
		// Node renderers are registered from the highest priority number
		// down, and each registration overwrites the last, so the *smaller*
		// number is the one that ends up rendering the node.
		util.Prioritized(crenderer.NewConfluenceFootnoteRenderer(c.Stdlib), 100),
	))

	// An <img> is a void tag, which the storage format -- being XML -- cannot
	// carry as written. Publishing one unconverted is how a page ends up
	// malformed rather than how it ends up without a picture, so this is a
	// correctness pass and not something to enable.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewHTMLImgTransformer(), 110),
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

	// Add math / latex formula support if requested
	if slices.Contains(c.MarkConfig.Features, "math") {
		m.Parser().AddOptions(parser.WithInlineParsers(
			// Ahead of goldmark's own inline parsers, so that a formula holding
			// markup characters -- and TeX is made of them -- is taken as a
			// formula rather than partly as emphasis or a link.
			util.Prioritized(cparser.NewMathParser(), 99),
		))

		m.Renderer().AddOptions(renderer.WithNodeRenderers(
			util.Prioritized(crenderer.NewConfluenceMathRenderer(c.Stdlib, c, c.MarkConfig), 100),
		))
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

	// Close the void elements and repair the comments an author may have
	// written by hand, which Markdown allows and storage format does not.
	// After everything at 110 that owns raw HTML of its own -- the <img> and
	// <details> transformers -- so that this only sees what they left behind,
	// and never turns an <img> into storage format the <img> transformer would
	// then fail to recognise.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(ctransformer.NewXMLWellFormedTransformer(), 120),
	))

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
