package renderer

import (
	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	cmath "github.com/kovetskiy/mark/v17/math"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// MathConfig is how a math renderer draws formulas.
type MathConfig struct {
	// Browser rasterises formulas published as PNG. It belongs to whoever
	// created it, who closes it; nil fails the first PNG formula.
	Browser *chrome.Browser
	// Format is the image a formula is published as, "png" (the default) or
	// "svg". Scale multiplies the pixels of a PNG one, and is ignored for SVG.
	Format cmath.Format
	Scale  float64
}

// ConfluenceMathRenderer publishes a LaTeX formula as an image.
//
// Confluence has no math of its own and no way to load the stylesheet that
// KaTeX or MathJax HTML would need, so a formula is rendered to SVG and
// uploaded beside the page, the way a mermaid or d2 diagram is. The formula's
// source is kept as the image's alt text, which is what makes it findable by
// search and readable to a screen reader.
type ConfluenceMathRenderer struct {
	Stdlib      *stdlib.Lib
	Attachments attachment.Attacher
	Math        MathConfig
	// attached remembers the formulas already queued for upload. The same
	// formula written twice on a page renders to the same file, and a renderer
	// lives for one document, so this keeps that one upload rather than one per
	// occurrence.
	attached map[string]bool
}

// NewConfluenceMathRenderer creates a new instance of the ConfluenceMathRenderer.
func NewConfluenceMathRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, cfg MathConfig) html.Extension {
	return &ConfluenceMathRenderer{
		Stdlib:      stdlib,
		Attachments: attachments,
		Math:        cfg,
		attached:    map[string]bool{},
	}
}

// RendererOptions implements html.Extension.
func (r *ConfluenceMathRenderer) RendererOptions(cfg *html.Config) []html.Option {
	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		cparser.KindMath:      nodeRenderer(r.renderMath),
		cparser.KindMathBlock: nodeRenderer(r.renderMathBlock),
	})}
}

// renderMathBlock publishes a display formula written on lines of its own. The
// picture is the same one an inline display formula gets; only where it sits on
// the page differs, and that is the block structure around it rather than
// anything here.
func (r *ConfluenceMathRenderer) renderMathBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n, ok := node.(*cparser.MathBlock)
	if !ok {
		return ast.WalkContinue, nil
	}

	return r.writeFormula(writer, n.Equation, true)
}

func (r *ConfluenceMathRenderer) renderMath(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*cparser.Math)

	return r.writeFormula(writer, n.Equation, n.Display)
}

// writeFormula renders one formula and writes the ac:image that shows it.
func (r *ConfluenceMathRenderer) writeFormula(writer util.BufWriter, equation []byte, display bool) (ast.WalkStatus, error) {
	// The error math.Process returns names the formula it could not render,
	// which locates it better than a line number would: a formula has no
	// position of its own in the AST, and a document usually has few enough
	// formulas that the source is the clearer identifier.
	rendered, err := cmath.Process(renderContext(), string(equation), cmath.Options{
		Display: display,
		Format:  r.Math.Format,
		Scale:   r.Math.Scale,
		Browser: r.Math.Browser,
	})
	if err != nil {
		return ast.WalkStop, err
	}

	if !r.attached[rendered.Filename] {
		r.attached[rendered.Filename] = true
		r.Attachments.Attach(rendered)
	}

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
			"",
			"",
			rendered.Width,
			rendered.Height,
			rendered.Width,
			rendered.Height,
			"",
			string(equation),
			rendered.Filename,
			"",
		},
	)
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}
