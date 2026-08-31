package renderer

import (
	"github.com/kovetskiy/mark/v16/attachment"
	cmath "github.com/kovetskiy/mark/v16/math"
	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

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
	MarkConfig  types.MarkConfig
	// attached remembers the formulas already queued for upload. The same
	// formula written twice on a page renders to the same file, and a renderer
	// lives for one document, so this keeps that one upload rather than one per
	// occurrence.
	attached map[string]bool
}

// NewConfluenceMathRenderer creates a new instance of the ConfluenceMathRenderer.
func NewConfluenceMathRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, cfg types.MarkConfig) renderer.NodeRenderer {
	return &ConfluenceMathRenderer{
		Stdlib:      stdlib,
		Attachments: attachments,
		MarkConfig:  cfg,
		attached:    map[string]bool{},
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceMathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(cparser.KindMath, r.renderMath)
}

func (r *ConfluenceMathRenderer) renderMath(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*cparser.Math)

	// The error math.Process returns names the formula it could not render,
	// which locates it better than a line number would: an inline formula has
	// no position of its own in the AST, and a document usually has few enough
	// formulas that the source is the clearer identifier.
	rendered, err := cmath.Process(string(n.Equation), n.Display, r.MarkConfig.MathFormat, r.MarkConfig.MathScale)
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
			// Escaped here rather than by the template, which leaves ac:alt
			// alone because its other caller escapes too. The formula is the
			// one attribute value on this element that comes straight out of
			// the document, and TeX is full of the characters that would end it.
			string(util.EscapeHTML(n.Equation)),
			rendered.Filename,
			"",
		},
	)
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}
