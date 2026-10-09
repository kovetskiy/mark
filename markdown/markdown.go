package mark

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// ConfluenceLegacyExtension is the original goldmark extension without GitHub Alerts support
// This extension is preserved for backward compatibility and testing purposes
//
// It is a goldmark parser.Extension and html.Extension at once: goldmark v2
// takes the two halves separately, the first to parser.New and the second to
// html.New.
type ConfluenceLegacyExtension struct {
	Stdlib      *stdlib.Lib
	Path        string
	MarkConfig  types.MarkConfig
	Attachments []attachment.Attachment
}

// NewConfluenceLegacyExtension creates a new instance of the ConfluenceLegacyExtension.
func NewConfluenceLegacyExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig) *ConfluenceLegacyExtension {
	return &ConfluenceLegacyExtension{
		Stdlib:      stdlib,
		Path:        path,
		MarkConfig:  cfg,
		Attachments: []attachment.Attachment{},
	}
}

func (c *ConfluenceLegacyExtension) Attach(a attachment.Attachment) {
	c.Attachments = append(c.Attachments, a)
}

// RendererOptions implements html.Extension.
//
// goldmark v2 keeps one renderer per node kind and lets the last registration
// for a kind win: html.New puts its own CommonMark renderers in first, then
// every extension in the order it was given. These come after goldmark's own
// table and strikethrough renderers, so they replace html.New's for every kind
// they name, which is what the smaller priority number did in v1.
func (c *ConfluenceLegacyExtension) RendererOptions(cfg *html.Config) []html.Option {
	renderers := []html.Extension{
		ctransformer.NewHTMLRenderer(),
		crenderer.NewConfluenceTextRenderer(c.MarkConfig.StripNewlines),
		crenderer.NewConfluenceBlockQuoteRenderer(),
		crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib),
		crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, c, c.MarkConfig, c.Path),
		crenderer.NewConfluenceHeadingRenderer(c.Stdlib, c.MarkConfig.DropFirstH1),
		crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceLinkRenderer(c.Stdlib, c, c.Path, c.MarkConfig.AttachReferenced),
		crenderer.NewConfluenceTaskListRenderer(),
		crenderer.NewConfluenceDefinitionListRenderer(),
		// goldmark's footnote parser is always on, so `[^1]` is always parsed.
		// The only question is what renders it, and the alternative is the
		// HTML goldmark's footnote renderer emits -- ids and fragment links,
		// neither of which survives a Confluence page. That renderer is not
		// registered at all.
		crenderer.NewConfluenceFootnoteRenderer(c.Stdlib),
	}
	renderers = append(renderers, featureRenderers(legacyFeatures, c.MarkConfig.Features, featureDeps{c.Stdlib, c, c.MarkConfig})...)

	return rendererOptions(cfg, renderers)
}

// ParserOptions implements parser.Extension.
func (c *ConfluenceLegacyExtension) ParserOptions(cfg *parser.Config) []parser.Option {
	opts := []parser.Option{
		parser.WithASTTransformers(ctransformer.ShapeTransformers()...),
		// <details> reaches here only because the document wrote the tag, and the
		// storage format has no way to carry it, so leaving it alone publishes
		// markup Confluence discards or rejects. There is nothing to opt into.
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](ctransformer.NewDetailsTransformer(), 110),
		),
	}

	opts = append(opts, featureParserOptions(legacyFeatures, c.MarkConfig.Features, cfg)...)

	opts = append(opts,
		// Close the void elements and repair the comments an author may have
		// written by hand, which Markdown allows and storage format does not.
		// After everything at 110 that owns raw HTML of its own, so that this only
		// sees what those transformers left behind.
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](ctransformer.NewXMLWellFormedTransformer(), 120),
		),
		parser.WithInlineParsers(
			// Must be registered with a smaller priority number than goldmark's
			// linkParser (200), so that it is tried first and goldmark doesn't
			// parse the <ac:*/> tags.
			util.Prioritized(cparser.NewConfluenceTagParser(), 199),
		),
	)

	return opts
}

// rendererOptions collects the options of each renderer in turn, the later
// ones replacing the earlier for any node kind both name. An extension cannot
// hand html.New further extensions to run, so this is how one extension
// carries several renderers.
func rendererOptions(cfg *html.Config, renderers []html.Extension) []html.Option {
	var opts []html.Option
	for _, r := range renderers {
		opts = append(opts, r.RendererOptions(cfg)...)
	}

	return opts
}

// markdownExtension is the half of each compile path goldmark is given.
type markdownExtension interface {
	parser.Extension
	html.Extension
}

// compileMarkdownWithExtension is a shared helper to eliminate code duplication
// between different compilation approaches
func compileMarkdownWithExtension(markdown []byte, ext markdownExtension, logMessage string) (string, error) {
	log.Trace().Msgf(logMessage, string(markdown))

	p := parser.New(
		parser.WithExtensions(
			extension.FootnoteParser,
			extension.DefinitionListParser,
			extension.TableParser,
			ext,
			extension.LinkifyParser,
			extension.StrikethroughParser,
			extension.TaskListItemParser,
		),
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
		parser.WithIDGenerator(cparser.ConfluenceIDGenerator{}),
	)

	r := html.New(
		html.WithUnsafe(),
		html.WithXHTML(),
		html.WithExtensions(
			// Alignment goes out as style="text-align:...", which Confluence
			// keeps, rather than as an align attribute.
			extension.NewTableHTMLRenderer(
				extension.WithTableCellAlignMethod(extension.TableCellAlignStyle),
			),
			extension.StrikethroughHTMLRenderer,
			// Last, so that its renderers are the ones that stay registered.
			// goldmark's own definition list, task list and footnote
			// renderers are left out: these replace every node kind they
			// render, and the task list one would replace the paragraph
			// renderer too.
			ext,
		),
	)

	var buf bytes.Buffer
	if err := r.Render(&buf, markdown, p.Parse(markdown)); err != nil {
		return "", err
	}

	html := buf.Bytes()
	log.Trace().Msgf("rendered markdown to html:\n%s", string(html))

	return string(html), nil
}

// pageTemplates is the template set one page's includes and macros are
// parsed into: a copy of the stdlib's, thrown away with the page.
//
// The stdlib's set is shared by every page in a run, and the renderers draw on
// it too. Fragments used to be parsed straight into it, each only once, so a
// fragment's {{ define "x" }} replaced "x" for every page after it: two
// directories each with a frag.md defining "hdr" published whichever had been
// read last on pages that included the other, and a fragment defining "ac:box"
// changed that macro across the run. A definition is still seen by the rest of
// the page that made it -- its macros and the fragments it includes after --
// since a shared file of definitions and macros using them is how includes
// are meant to be used; it just ends with the page.
//
// A fragment is therefore read and parsed once per page rather than once per
// run.
func pageTemplates(lib *stdlib.Lib) (*template.Template, error) {
	if lib == nil {
		return template.New("stdlib"), nil
	}

	tmpl, err := lib.Templates.Clone()
	if err != nil {
		return nil, fmt.Errorf("unable to copy the stdlib templates: %w", err)
	}

	return tmpl, nil
}

// maxIncludePasses bounds the number of times the whole document is rescanned
// for include directives. ProcessIncludes drains every directive it can see in
// one pass, so a pass that still finds work is expansion feeding itself, and
// without a bound the document grows until the process is killed.
const maxIncludePasses = 10

// expandDirectives drains every include and macro the document names, and
// everything they name in turn, before goldmark is given the bytes.
//
// The two produce each other's work: an included fragment may define a macro,
// and a macro's expansion may include a file. Running includes once and then
// macros once left the second kind unexpanded, and the AST transformers were
// what picked it up afterwards -- splicing a sub-document, parsed from its own
// bytes, into an AST that is rendered against the document's. Every node those
// two did not rewrite kept offsets into the wrong buffer, so a fenced code
// block in an included fragment came out with its language read from the middle
// of the directive, a body sliced from wherever those offsets happened to land,
// and NUL bytes where the slice ran past the end -- illegal in XML, so the page
// was refused.
//
// Draining them here means the AST never sees a directive, which is what
// invariant 3 in AGENTS.md says should be true.
func expandDirectives(
	path string,
	cfg types.MarkConfig,
	markdown []byte,
	tmpl *template.Template,
) (*template.Template, []byte, []string, error) {
	var attachments []string

	resolve := macroFileName(filepath.Dir(path))

	for pass := 0; pass < maxIncludePasses; pass++ {
		before := markdown

		var err error

		tmpl, markdown, err = expandIncludes(path, cfg.IncludePath, markdown, tmpl)
		if err != nil {
			return nil, nil, nil, err
		}

		var macros []macro.Macro

		macros, markdown, err = macro.ExtractMacros(filepath.Dir(path), cfg.IncludePath, markdown, tmpl)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("unable to extract macros: %w", err)
		}

		for _, m := range macros {
			var attached []string

			markdown, attached, err = m.ApplyCollecting(markdown, resolve)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("unable to apply macro %q: %w", m.Regexp.String(), err)
			}

			for _, name := range attached {
				if !slices.Contains(attachments, name) {
					attachments = append(attachments, name)
				}
			}
		}

		// Nothing left to find. Compared by content rather than by counting
		// what was applied, since a macro that matches nothing still counts as
		// applied and would keep the loop going for ever.
		if bytes.Equal(before, markdown) {
			return tmpl, markdown, attachments, nil
		}
	}

	return nil, nil, nil, fmt.Errorf(
		"includes and macros did not settle after %d passes over %q", maxIncludePasses, path,
	)
}

// macroFileName reads a macro's Attachment value the way an image destination is
// read, and says whether it names a file at all: a URL or a rooted path does
// not. A value ending in an image title is tried without it first, then as
// written; each spelling as written, then percent-decoded. The macro writes the
// returned name into the page, so it has to be settled before it does.
//
// The boundary comes before every lookup: a name that reaches outside the
// project must not learn from os.Stat whether the file is there. Such a name is
// handed on, for the upload to refuse, only when it is the destination itself
// and nothing inside the project answered; a title that reaches outside is just
// a title.
func macroFileName(base string) func(string) (string, bool) {
	return func(value string) (string, bool) {
		return readMacroFileName(base, value)
	}
}

// readMacroFileName is macroFileName for one value.
func readMacroFileName(base, name string) (string, bool) {
	destinations := []string{name}
	if match := imageTitle.FindStringSubmatch(name); match != nil {
		destinations = []string{match[1], name}
	}

	if !writtenAsFile(destinations[0]) {
		return stripBrackets(name), false
	}

	var outside string

	for i, destination := range destinations {
		for _, candidate := range ctransformer.LocalImagePaths(macroDestination(destination)) {
			if !crenderer.NamesBesideDocument(candidate) {
				continue
			}

			if attachment.CheckReadable(base, candidate) != nil {
				if i == 0 && outside == "" {
					outside = candidate
				}

				continue
			}

			if info, err := os.Stat(filepath.Join(base, candidate)); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
	}

	if outside != "" {
		return outside, true
	}

	// Nothing is there. The warning and the page name the file the
	// destination does: no brackets, no escapes, and no title. A value that
	// stops naming a file beside the document once read is kept as written,
	// for the upload to warn about by that name.
	if cleaned := macroDestination(destinations[0]); crenderer.NamesBesideDocument(cleaned) {
		return cleaned, true
	}

	return name, true
}

// writtenAsFile reports whether a destination, brackets taken off, is written
// as a file beside the document rather than as a URL or a rooted path.
//
// A leading backslash is a Windows root, or the "\\server" of a UNC path,
// except where it escapes some other punctuation: "\/etc/passwd" is
// "/etc/passwd" spelled with an escape, as "%2Fetc%2Fpasswd" and
// "&#47;etc&#47;passwd" are, and is warned about as they are rather than taken
// for a path someone meant to write.
func writtenAsFile(destination string) bool {
	destination = stripBrackets(destination)
	if crenderer.NamesBesideDocument(destination) {
		return true
	}

	// util.IsPunct is exactly the ASCII punctuation CommonMark lets a
	// backslash escape.
	return len(destination) > 1 && destination[0] == '\\' &&
		destination[1] != '\\' && util.IsPunct(destination[1])
}

// stripBrackets takes the angle brackets off a destination. "<my file.png>" is
// how Markdown writes a destination with a space in it; goldmark takes the
// brackets off an image's, but a macro sees the raw text.
func stripBrackets(destination string) string {
	if len(destination) > 2 && destination[0] == '<' && destination[len(destination)-1] == '>' {
		return destination[1 : len(destination)-1]
	}

	return destination
}

// macroDestination takes the brackets off a destination and resolves its
// backslash escapes and entities.
func macroDestination(destination string) string {
	return ctransformer.UnescapeDestination(stripBrackets(destination))
}

// imageTitle matches a destination followed by an image title, in any of the
// three quotings CommonMark allows, and captures the destination. A title may
// hold its own delimiter backslash-escaped -- "a \"b\" c", 'Bob\'s', (a \) b) --
// but not a bare parenthesis inside parentheses: CommonMark does not read
// ![x](logo.png (a (b))) as an image at all, so there is no title to drop.
var imageTitle = regexp.MustCompile(
	`^(.*\S)\s+(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\((?:[^()\\]|\\.)*\))$`,
)

// attachMacroFiles uploads the files macros name in their Attachment key, which
// the expansion has already written into the page under their flattened names.
//
// Every name is one macroFileName said is a file: a URL or a rooted path never
// gets here. A file the page declared is skipped, and counted as used by the
// lookup. A file that is not there is only warned about, as it may already be on
// the page; one outside the project fails, as it does anywhere else.
func attachMacroFiles(path string, cfg types.MarkConfig, names []string) ([]attachment.Attachment, error) {
	var attached []attachment.Attachment

	for _, name := range names {
		if cfg.ResolveAttachment != nil && cfg.ResolveAttachment(name) != "" {
			continue
		}

		// "\/etc/passwd" is written as a file but reads as a rooted path, which
		// must never be joined onto the document's directory to be looked for.
		if !crenderer.NamesBesideDocument(name) {
			log.Warn().Msgf("macro attachment %q is not uploaded: it does not name a file beside the document", name)

			continue
		}

		file, err := attachment.ResolveLocalAttachment(vfs.LocalOS, filepath.Dir(path), name)
		if errors.Is(err, attachment.ErrOutsideProject) {
			return nil, fmt.Errorf("unable to attach %q named by a macro: %w", name, err)
		}

		if err != nil {
			log.Warn().Err(err).Msgf("macro attachment %q is not uploaded", name)

			continue
		}

		attached = append(attached, file)
	}

	return attached, nil
}

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
	tmpl, err := pageTemplates(stdlib)
	if err != nil {
		return "", nil, err
	}

	// The page's set is handed on to the AST include and macro transformers,
	// so that they see what the page's own fragments defined. The renderers
	// keep drawing on the stdlib itself.
	_, markdown, macroFiles, err := expandDirectives(path, cfg, markdown, tmpl)
	if err != nil {
		return "", nil, err
	}

	macroAttachments, err := attachMacroFiles(path, cfg, macroFiles)
	if err != nil {
		return "", nil, err
	}

	ghAlertsExtension := newConfluenceExtension(stdlib, path, cfg, tmpl)
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

	htmlOutput, replaced := sanitizeXMLChars(htmlOutput)
	warnIllegalXMLChars(path, markdown, replaced)

	return htmlOutput, append(macroAttachments, ghAlertsExtension.Attachments...), nil
}

// CompileMarkdownLegacy compiles markdown using the legacy approach without GitHub Alerts transformer
// This function is preserved for backward compatibility and testing purposes
func CompileMarkdownLegacy(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	tmpl, err := pageTemplates(stdlib)
	if err != nil {
		return "", nil, err
	}

	// The template set the expansion built is not carried forward: the legacy
	// extension runs no include or macro transformer to hand it to.
	_, markdown, macroFiles, err := expandDirectives(path, cfg, markdown, tmpl)
	if err != nil {
		return "", nil, err
	}

	macroAttachments, err := attachMacroFiles(path, cfg, macroFiles)
	if err != nil {
		return "", nil, err
	}

	confluenceExtension := NewConfluenceLegacyExtension(stdlib, path, cfg)
	htmlOutput, err := compileMarkdownWithExtension(markdown, confluenceExtension, "rendering markdown with legacy renderer:\n%s")
	if err != nil {
		return "", nil, err
	}

	htmlOutput, replaced := sanitizeXMLChars(htmlOutput)
	warnIllegalXMLChars(path, markdown, replaced)

	return htmlOutput, append(macroAttachments, confluenceExtension.Attachments...), nil
}

// ConfluenceExtension is a goldmark extension for GitHub Alerts with Transformer approach
// This extension provides superior GitHub Alert processing by transforming [!NOTE], [!TIP], etc.
// into proper Confluence macros while maintaining full compatibility with existing functionality.
// This is now the primary/default extension.
//
// Like ConfluenceLegacyExtension it is a parser.Extension and an
// html.Extension at once.
type ConfluenceExtension struct {
	Stdlib          *stdlib.Lib
	Path            string
	MarkConfig      types.MarkConfig
	Attachments     []attachment.Attachment
	Pipeline        *ctransformer.PipelineTransformer
	Links           *ctransformer.LinkTransformer
	AttachmentLinks *ctransformer.AttachmentTransformer
}

// NewConfluenceExtension creates a new instance of the ConfluenceExtension, the
// extension with GitHub Alerts support.
// It is the standalone version that does not depend on feature flags.
func NewConfluenceExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig) *ConfluenceExtension {
	var tmpl *template.Template
	if stdlib != nil {
		// A page's own copy, for the reason pageTemplates gives. Cloning a
		// text/template set cannot fail once it has been parsed, which the
		// stdlib's has.
		tmpl, _ = stdlib.Templates.Clone()
	}

	return newConfluenceExtension(stdlib, path, cfg, tmpl)
}

// newConfluenceExtension is NewConfluenceExtension with the template set the
// include and macro transformers parse into given by the caller.
func newConfluenceExtension(stdlib *stdlib.Lib, path string, cfg types.MarkConfig, tmpl *template.Template) *ConfluenceExtension {
	pipeline := ctransformer.NewPipelineTransformer(
		// The base directory a template path is resolved against is the
		// document's directory, not the document itself. Passing path for both
		// made every relative Template: resolve under "doc.md/", which no
		// filesystem has, so the AST macro and include transformers only ever
		// worked when --include-path happened to cover the file.
		ctransformer.NewMacroTransformer(path, filepath.Dir(path), cfg.IncludePath, tmpl),
		ctransformer.NewIncludeTransformer(path, filepath.Dir(path), cfg.IncludePath, tmpl),
	)
	return &ConfluenceExtension{
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

// RendererOptions implements html.Extension. It registers, in order:
// 1. Core renderers for standard markdown elements
// 2. GitHub Alerts specific renderers (blockquote and text)
// 3. The renderers of the features that are switched on
//
// Each replaces html.New's renderer for the node kinds it names, as the
// legacy extension's do.
func (c *ConfluenceExtension) RendererOptions(cfg *html.Config) []html.Option {
	renderers := []html.Extension{
		// The nodes the transformers stand in for goldmark v1's with.
		ctransformer.NewHTMLRenderer(),

		// Core renderers (excluding blockquote and text which are replaced below)
		crenderer.NewConfluenceCodeBlockRenderer(c.Stdlib),
		crenderer.NewConfluenceFencedCodeBlockRenderer(c.Stdlib, c, c.MarkConfig, c.Path),
		crenderer.NewConfluenceHeadingRenderer(c.Stdlib, c.MarkConfig.DropFirstH1),
		crenderer.NewConfluenceImageRenderer(c.Stdlib, c, c.Path, c.MarkConfig.ImageAlign),
		crenderer.NewConfluenceParagraphRenderer(),
		crenderer.NewConfluenceLinkRenderer(c.Stdlib, c, c.Path, c.MarkConfig.AttachReferenced),
		crenderer.NewConfluenceTaskListRenderer(),
		crenderer.NewConfluenceDefinitionListRenderer(),

		// GitHub Alerts specific renderers. These handle both GitHub Alerts
		// and legacy blockquote syntax.
		crenderer.NewConfluenceGHAlertsBlockQuoteRenderer(),
		crenderer.NewConfluenceTextRenderer(c.MarkConfig.StripNewlines),

		// goldmark's footnote parser is always on, so `[^1]` is always parsed.
		// The only question is what renders it, and the alternative is the HTML
		// goldmark's footnote renderer emits -- ids and fragment links, neither
		// of which survives a Confluence page. That renderer is not registered
		// at all.
		crenderer.NewConfluenceFootnoteRenderer(c.Stdlib),
	}
	renderers = append(renderers, featureRenderers(defaultFeatures, c.MarkConfig.Features, featureDeps{c.Stdlib, c, c.MarkConfig})...)

	return rendererOptions(cfg, renderers)
}

// ParserOptions implements parser.Extension. It registers the AST
// transformers for macros, includes, layouts and GitHub Alerts, and the
// parsers of the features that are switched on.
func (c *ConfluenceExtension) ParserOptions(cfg *parser.Config) []parser.Option {
	opts := []parser.Option{
		parser.WithASTTransformers(ctransformer.ShapeTransformers()...),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](c.Pipeline, 10),
			util.Prioritized[parser.ASTTransformer](ctransformer.NewLayoutTransformer(), 100),
			util.Prioritized[parser.ASTTransformer](ctransformer.NewGHAlertsTransformer(), 100),
			// Last, so that it sees the headings includes and macros brought in as
			// well as the ones written in the file, and so that heading ids have
			// already been assigned.
			util.Prioritized[parser.ASTTransformer](ctransformer.NewAnchorTransformer(), 900),
			// After the anchor transformer, so a heading that a manual anchor sits
			// beside has already been matched to the links that name it.
			util.Prioritized[parser.ASTTransformer](ctransformer.NewManualAnchorTransformer(), 901),
			// After includes and macros have brought their content in, so links
			// inside an included fragment are resolved too.
			// Before link resolution, so that a path a document declared as an
			// attachment is taken as one rather than looked up as a page.
			util.Prioritized[parser.ASTTransformer](c.AttachmentLinks, 905),
			util.Prioritized[parser.ASTTransformer](c.Links, 910),
		),
	}

	// <details> reaches here only because the document wrote the tag, and the
	// storage format has no way to carry it, so leaving it alone publishes
	// markup Confluence discards or rejects. There is nothing to opt into.
	opts = append(opts, parser.WithASTTransformers(
		util.Prioritized[parser.ASTTransformer](ctransformer.NewDetailsTransformer(), 110),
	))

	// An <img> is a void tag, which the storage format -- being XML -- cannot
	// carry as written. Publishing one unconverted is how a page ends up
	// malformed rather than how it ends up without a picture, so this is a
	// correctness pass and not something to enable.
	//
	// After the <details> and layout transformers, which rewrite a block they
	// own into a Text node of markup, so that an <img> inside one is found
	// there. At the same number as <details> the order between the two was
	// whatever the sort left it.
	opts = append(opts, parser.WithASTTransformers(
		util.Prioritized[parser.ASTTransformer](ctransformer.NewHTMLImgTransformer(), 115),
	))

	opts = append(opts, featureParserOptions(defaultFeatures, c.MarkConfig.Features, cfg)...)

	opts = append(opts,
		// Close the void elements, quote the attributes, escape the bare "&"s and
		// repair the comments an author may have written by hand, which Markdown
		// allows and storage format does not.
		// After everything below it that owns raw HTML of its own -- the <img> and
		// <details> transformers, and the manual anchor one at 901 -- so that this
		// only sees what they left behind, and never turns an <img> or an
		// <a id="a&b"> into storage format the transformer that owns it would then
		// fail to recognise.
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](ctransformer.NewXMLWellFormedTransformer(), 950),
		),
		// Add confluence tag parser for <ac:*/> tags, tried before goldmark's
		// link parser at 200.
		parser.WithInlineParsers(
			util.Prioritized(cparser.NewConfluenceTagParser(), 199),
		),
	)

	return opts
}

// CompileMarkdownWithTransformer compiles markdown using the transformer approach for GitHub Alerts
// This function provides enhanced GitHub Alert processing while maintaining full compatibility
// with existing markdown functionality. It transforms [!NOTE], [!TIP], etc. into proper titles.
// This is an alias for CompileMarkdown for backward compatibility.
func CompileMarkdownWithTransformer(markdown []byte, stdlib *stdlib.Lib, path string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	return CompileMarkdown(markdown, stdlib, path, cfg)
}
