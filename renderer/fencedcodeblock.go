package renderer

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/d2"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/kovetskiy/mark/v17/mermaid"
	"github.com/kovetskiy/mark/v17/stdlib"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// DiagramConfig is how the fenced code block renderer draws the
// diagrams it is switched on for.
type DiagramConfig struct {
	// Browser rasterises d2 diagrams published as PNG, and Mermaid draws
	// mermaid diagrams. Both belong to whoever created them, who closes them;
	// nil fails the first diagram that needs one.
	Browser *chrome.Browser
	Mermaid *mermaid.Engine

	// Features names the diagram kinds handled: "d2", "mermaid" and
	// "plantuml". A block in a kind not named is shown as code.
	Features []string
	// ImageAlign is where a diagram sits on the page: "left", "center",
	// "right", or "" for Confluence's default.
	ImageAlign string

	// MermaidOutput is the image a diagram is published as, PNG (the
	// default) or SVG. MermaidScale multiplies the pixels of a PNG one and
	// the size the page displays an SVG one at. MermaidBundle keeps the
	// diagram's source inside the SVG it is published as, and applies to
	// nothing else.
	MermaidOutput imageformat.Format
	MermaidScale  float64
	MermaidBundle bool

	// D2Output is the image a diagram is published as, PNG (the default)
	// or SVG. D2Scale multiplies the pixels of a PNG one and the size the
	// page displays an SVG one at. D2BundleRemote allows a diagram to have mark
	// fetch a URL it names and publish what comes back inside the drawing.
	D2Output       imageformat.Format
	D2Scale        float64
	D2BundleRemote bool
}

type ConfluenceFencedCodeBlockRenderer struct {
	htmlOptions

	ctx         context.Context
	Stdlib      *stdlib.Lib
	Diagrams    DiagramConfig
	Attachments attachment.Attacher
	// Path is the document the block was written in, which is what a d2
	// diagram's imports and local images are relative to. Empty is allowed: d2
	// reads "-" as a diagram that came from nowhere on disk, and resolves
	// nothing local against it.
	Path string
}

// NewConfluenceFencedCodeBlockRenderer creates a new instance of the
// ConfluenceFencedCodeBlockRenderer. ctx bounds the diagrams it draws: goldmark
// renders without a context, and a renderer lives for one document.
func NewConfluenceFencedCodeBlockRenderer(ctx context.Context, stdlib *stdlib.Lib, attachments attachment.Attacher, cfg DiagramConfig, path string, opts ...html.Option) html.Extension {
	r := &ConfluenceFencedCodeBlockRenderer{
		ctx:         ctx,
		Stdlib:      stdlib,
		Diagrams:    cfg,
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

	if lang == "d2" && slices.Contains(r.Diagrams.Features, "d2") {
		// The document's own path, empty and all: a diagram that came from
		// nowhere on disk has nothing for a relative import or image to be
		// relative to, and d2.Render refuses one rather than reading it from
		// the working directory. It reads an empty path and d2's own "-"
		// alike. Render also refuses a format it does not
		// know, which Run checks but options handed straight to
		// CompileMarkdown never pass through.
		attachment, err := d2.Render(orBackground(r.ctx), title, lval, d2.Options{
			Output:       r.Diagrams.D2Output,
			Scale:        r.Diagrams.D2Scale,
			Browser:      r.Diagrams.Browser,
			InputPath:    r.Path,
			BundleRemote: r.Diagrams.D2BundleRemote,
		})
		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: d2 rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(attachment)

		effectiveAlign := calculateAlign(r.Diagrams.ImageAlign, attachment.Width)
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
				// no "title" in the info string, d2.Render and mermaid's Render fall back
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

	} else if lang == "mermaid" && slices.Contains(r.Diagrams.Features, "mermaid") {
		// Render refuses a format it does not know, as the d2 branch does.
		att, err := r.Diagrams.Mermaid.Render(orBackground(r.ctx), title, lval, mermaid.Options{
			Output: r.Diagrams.MermaidOutput,
			Scale:  r.Diagrams.MermaidScale,
			Bundle: r.Diagrams.MermaidBundle,
		})
		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: mermaid rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(att)

		effectiveAlign := calculateAlign(r.Diagrams.ImageAlign, att.Width)
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
				// no "title" in the info string, d2.Render and mermaid's Render fall back
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

	} else if lang == "plantuml" && slices.Contains(r.Diagrams.Features, "plantuml") {
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
