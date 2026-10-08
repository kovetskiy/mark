package renderer

import (
	"io"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// Every Confluence renderer here is an html.Extension: goldmark v2 has no
// prioritised node renderers, only a node kind -> renderer map that each
// extension's RendererOptions writes into in turn. html.New registers its own
// CommonMark renderers first, so an extension given to it afterwards replaces
// them for the kinds it names, and of two extensions naming one kind the later
// one wins.

// renderFunc is a node renderer that writes plain bytes and has no use for the
// render context.
type renderFunc func(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error)

// contextRenderFunc is a node renderer that needs the render context, for the
// writers goldmark escapes text through.
type contextRenderFunc func(w util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error)

// nodeRenderer adapts a renderFunc to goldmark's html.NodeRenderer. The writer
// html.Renderer hands a node renderer is always a util.BufWriter.
func nodeRenderer(f renderFunc) html.NodeRenderer {
	return html.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
		return f(w.(util.BufWriter), source, node, entering)
	})
}

// contextNodeRenderer adapts a contextRenderFunc to goldmark's html.NodeRenderer.
func contextNodeRenderer(f contextRenderFunc) html.NodeRenderer {
	return html.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
		return f(w.(util.BufWriter), source, node, entering, rc)
	})
}

// withOptions is cfg with the options a renderer's constructor was given
// applied on top. Those are the html options this repo's renderers have always
// taken; the ones html.New is given reach a renderer through cfg.
func withOptions(cfg html.Config, opts []html.Option) html.Config {
	for _, opt := range opts {
		opt.SetFormatOption(&cfg)
	}

	return cfg
}
