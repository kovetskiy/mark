package renderer

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/d2"
	"github.com/kovetskiy/mark/v16/internal/syntax"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceFencedCodeBlockRenderer struct {
	htmlOptions

	Stdlib      *stdlib.Lib
	MarkConfig  types.MarkConfig
	Attachments attachment.Attacher
	// Path is the document the block was written in, which is what a local
	// image referenced by a d2 diagram is relative to. Empty is allowed: d2
	// reads "-" as a diagram that came from nowhere on disk, and resolves
	// nothing local against it.
	Path string
}

// NewConfluenceFencedCodeBlockRenderer creates a new instance of the ConfluenceFencedCodeBlockRenderer.
func NewConfluenceFencedCodeBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, cfg types.MarkConfig, path string, opts ...html.Option) html.Extension {
	r := &ConfluenceFencedCodeBlockRenderer{
		Stdlib:      stdlib,
		MarkConfig:  cfg,
		Attachments: attachments,
		Path:        path,
	}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceFencedCodeBlockRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRendererDecorator(ast.KindCodeBlock, r.decorateCodeBlock)}
}

// decorateCodeBlock takes the fenced code blocks for this renderer. goldmark
// has one node kind for fenced and indented blocks alike, and the indented
// ones are left to whichever renderer the kind is registered to -- the
// ConfluenceCodeBlockRenderer, in both compile paths.
func (r *ConfluenceFencedCodeBlockRenderer) decorateCodeBlock(next html.NodeRenderer) html.NodeRenderer {
	fenced := nodeRenderer(r.renderFencedCodeBlock)

	return html.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
		if block, ok := node.(*ast.CodeBlock); ok && block.CodeBlockKind == ast.CodeBlockKindFenced {
			return fenced.Render(w, source, node, entering, rc)
		}

		return next.Render(w, source, node, entering, rc)
	})
}

// renderFencedCodeBlock renders a FencedCodeBlock
func (r *ConfluenceFencedCodeBlockRenderer) renderFencedCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	// The info string as written: what it holds is mark's own option
	// vocabulary, which reads no escapes.
	nodeFencedCodeBlock := node.(*ast.CodeBlock)
	info := nodeFencedCodeBlock.Info.Bytes(source)
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false

	lang, options, title := syntax.ParseCodeBlockInfo(string(info))
	for _, option := range options {
		if option == "collapse" {
			collapse = true
			continue
		}
		if option == "nocollapse" {
			collapse = false
			continue
		}
		if option == "linenumbers" {
			linenumbers = true
			continue
		}

		// strconv.Atoi rather than fmt.Sscanf("%d"): Sscanf stops at the first
		// byte it cannot use and reports success for what it read, so an
		// option that merely begins with a digit -- "1;" or the `2"` left by a
		// split hl_lines="1 2" -- silently turned line numbering on and moved
		// the first line. Only an option that is a number all through is one.
		if i, err := strconv.Atoi(option); err == nil {
			linenumbers = i > 0
			firstline = i
			continue
		}
		theme = option
	}

	lval := slices.Clip(nodeFencedCodeBlock.Value.Bytes(source))

	if lang == "d2" && slices.Contains(r.MarkConfig.Features, "d2") {
		var (
			attachment attachment.Attachment
			err        error
		)

		switch r.MarkConfig.D2Output {
		case "svg":
			// The document's own path, empty and all: a diagram that came from
			// nowhere on disk has nothing for a relative reference to be
			// relative to, and d2 resolves one against the working directory
			// rather than refusing it. ProcessD2SVG reads an empty path and
			// d2's own "-" alike, so neither reaches the bundler.
			attachment, err = d2.ProcessD2SVG(
				title, lval, r.Path, r.MarkConfig.D2Scale, r.MarkConfig.D2BundleRemote,
			)

		case "png", "":
			attachment, err = d2.ProcessD2(r.MarkConfig.Browser, title, lval, r.MarkConfig.D2Scale)

		default:
			// Run checks this, but CompileMarkdown takes a types.MarkConfig
			// straight from a caller and never passes through it. Publishing a
			// PNG for a format nobody asked for is the one answer that says
			// nothing, so it is refused here too.
			line, col := GetLineCol(source, node.Pos())

			return ast.WalkStop, fmt.Errorf(
				"line %d, col %d: unknown d2-output %q: expected png or svg",
				line, col, r.MarkConfig.D2Output,
			)
		}

		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: d2 rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(attachment)

		effectiveAlign := calculateAlign(r.MarkConfig.ImageAlign, attachment.Width)
		effectiveLayout := calculateLayout(effectiveAlign, attachment.Width)
		displayWidth := calculateDisplayWidth(attachment.Width, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				effectiveAlign,
				effectiveLayout,
				attachment.Width,
				attachment.Height,
				displayWidth,
				"",
				// The display title, not the attachment name: when the author gave
				// no "title" in the info string, ProcessD2/ProcessMermaid fall back
				// to the content checksum for the filename, and passing that here
				// made Confluence render a 64-character hash as a caption under the
				// diagram. The attachment keeps its checksum-derived name.
				title,
				"",
				attachment.Filename,
				"",
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else if lang == "mermaid" && slices.Contains(r.MarkConfig.Features, "mermaid") {
		var (
			att attachment.Attachment
			err error
		)

		switch {
		case r.MarkConfig.MermaidOutput == "svg" && r.MarkConfig.MermaidBundle:
			att, err = r.MarkConfig.Mermaid.ProcessMermaidWithBundle(title, lval, r.MarkConfig.MermaidScale)

		case r.MarkConfig.MermaidOutput == "svg":
			att, err = r.MarkConfig.Mermaid.ProcessMermaidSVG(title, lval, r.MarkConfig.MermaidScale)

		case r.MarkConfig.MermaidOutput == "png", r.MarkConfig.MermaidOutput == "":
			att, err = r.MarkConfig.Mermaid.ProcessMermaidLocally(title, lval, r.MarkConfig.MermaidScale)

		default:
			// Run checks this, but CompileMarkdown takes a types.MarkConfig
			// straight from a caller and never passes through it. Publishing a
			// PNG for a format nobody asked for is the one answer that says
			// nothing, so it is refused here too -- the same backstop the d2
			// branch above keeps.
			line, col := GetLineCol(source, node.Pos())

			return ast.WalkStop, fmt.Errorf(
				"line %d, col %d: unknown mermaid-output %q: expected png or svg",
				line, col, r.MarkConfig.MermaidOutput,
			)
		}

		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: mermaid rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(att)

		effectiveAlign := calculateAlign(r.MarkConfig.ImageAlign, att.Width)
		effectiveLayout := calculateLayout(effectiveAlign, att.Width)
		displayWidth := calculateDisplayWidth(att.Width, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				effectiveAlign,
				effectiveLayout,
				att.Width,
				att.Height,
				displayWidth,
				"",
				// The display title, not the attachment name: when the author gave
				// no "title" in the info string, ProcessD2/ProcessMermaid fall back
				// to the content checksum for the filename, and passing that here
				// made Confluence render a 64-character hash as a caption under the
				// diagram. The attachment keeps its checksum-derived name.
				title,
				"",
				att.Filename,
				"",
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else if lang == "plantuml" && slices.Contains(r.MarkConfig.Features, "plantuml") {
		err := r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:plantuml",
			struct {
				Text string
			}{
				strings.TrimSuffix(string(lval), "\n"),
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else {
		err := r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:code",
			struct {
				Language    string
				Collapse    bool
				Title       string
				Theme       string
				Linenumbers bool
				Firstline   int
				Text        string
			}{
				syntax.CodeLanguage(lang),
				collapse,
				title,
				theme,
				linenumbers,
				firstline,
				strings.TrimSuffix(string(lval), "\n"),
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}
	}

	return ast.WalkContinue, nil
}
