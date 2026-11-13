package markdown

import (
	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/kovetskiy/mark/v17/mermaid"
	crenderer "github.com/kovetskiy/mark/v17/renderer"
	"github.com/kovetskiy/mark/v17/report"
)

// Options is how CompileMarkdown turns a document into storage format. The
// zero value switches every optional feature off, mermaid and mention
// included, so it draws no pictures and needs no browser.
type Options struct {
	// Browser rasterises d2 diagrams and math formulas published as PNG, and
	// Mermaid draws mermaid diagrams. Both start on first use and belong to
	// whoever created them, who closes them; nil fails the first picture that
	// needs one.
	Browser *chrome.Browser
	Mermaid *mermaid.Engine

	// DropH1 leaves the document's first H1 out of the page, for a page
	// whose title already says it.
	DropH1 bool
	// StripLinebreaks writes a paragraph's soft line breaks as spaces
	// rather than newlines.
	StripLinebreaks bool

	// MermaidOutput is how a diagram is published: PNG (the default) or SVG
	// drawn by mark, or Macro, where the source is published as the instance's
	// own macro. MermaidScale multiplies the pixels of a PNG one and the size
	// the page displays an SVG one at, and applies to neither with Macro.
	// MermaidBundle keeps the diagram's source inside the SVG it is published
	// as, and applies to nothing else.
	MermaidOutput mermaid.Output
	MermaidScale  float64
	MermaidBundle bool
	// MermaidMacroName is the ac:name a diagram is published under with Macro;
	// blank means mermaid.DefaultMacroName.
	MermaidMacroName string
	// D2Output is the image a diagram is published as, PNG (the default)
	// or SVG. D2Scale multiplies the pixels of a PNG one and the size the
	// page displays an SVG one at.
	D2Output imageformat.Format
	D2Scale  float64
	// D2BundleRemote allows a diagram to have mark fetch a URL it names and
	// publish what comes back inside the drawing.
	D2BundleRemote bool
	// MathOutput is the image a formula is published as, PNG (the default)
	// or SVG. MathScale multiplies the pixels of a PNG one, and is ignored
	// for SVG.
	MathOutput imageformat.Format
	MathScale  float64

	// Features names the optional features switched on; see --features.
	Features []string
	// ImageAlign is where images and diagrams sit on the page: "left",
	// "center", "right", or "" for Confluence's default.
	ImageAlign string

	// AttachReferenced uploads a file a link points at, rather than leaving the
	// link pointing at a path that means nothing once the page is published.
	// Images are attached whether this is set or not, since an image left as a
	// path is visibly broken where a link merely leads nowhere.
	AttachReferenced bool
	// IncludePath is where includes and macros not found beside the
	// document are looked for.
	IncludePath string

	// Root is where the files the document names are read from -- images,
	// linked files, macro attachments, and the includes and macro templates
	// beside it -- relative to the directory of the path it is compiled as.
	// The zero Root is the local disk. A d2 diagram's imports and local
	// images are read from it too.
	Root attachment.Root

	// ResolveLink turns a link target written in the document -- a relative
	// path, optionally with a #fragment -- into the Confluence link it should
	// become, or "" to leave it as written. The text is the words between the
	// brackets, which only an ac: link with nothing after the colon uses.
	//
	// Supplied as a function so that rewriting links stays a property of the
	// document tree rather than of the bytes it was parsed from, without the
	// renderer needing to know what Confluence is. Nil disables the rewriting
	// entirely, which is what compiling without a server does.
	ResolveLink func(target, text string) (string, error)

	// ResolveAttachment turns a link or image destination into the URL of the
	// attachment uploaded for it, or "" to leave it as written. Nil disables
	// the rewriting.
	ResolveAttachment func(target string) string

	// Warnings collects what was wrong with the document but did not stop it
	// compiling: a macro attachment not uploaded, a Column-Widths comment not
	// applied, characters XML cannot carry. Nil only logs them, against the
	// document's path.
	Warnings *report.Warnings
}

// diagramConfig is the part of o the fenced code block renderer draws with.
func (o Options) diagramConfig() crenderer.DiagramConfig {
	return crenderer.DiagramConfig{
		Browser:          o.Browser,
		Mermaid:          o.Mermaid,
		Features:         o.Features,
		ImageAlign:       o.ImageAlign,
		MermaidOutput:    o.MermaidOutput,
		MermaidMacroName: o.MermaidMacroName,
		MermaidScale:     o.MermaidScale,
		MermaidBundle:    o.MermaidBundle,
		D2Output:         o.D2Output,
		D2Scale:          o.D2Scale,
		D2BundleRemote:   o.D2BundleRemote,
	}
}

// mathConfig is the part of o the math renderer draws with.
func (o Options) mathConfig() crenderer.MathConfig {
	return crenderer.MathConfig{
		Browser: o.Browser,
		Output:  o.MathOutput,
		Scale:   o.MathScale,
	}
}
