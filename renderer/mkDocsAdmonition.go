package renderer

import (
	"fmt"

	parser "github.com/stefanfritsch/goldmark-admonitions"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// HeadingAttributeFilter defines attribute names which heading elements can have
var MkDocsAdmonitionAttributeFilter = html.GlobalAttributeFilter

// A Renderer struct is an implementation of renderer.NodeRenderer that renders
// nodes as (X)HTML.
type ConfluenceMkDocsAdmonitionRenderer struct {
	html.Config
	LevelMap MkDocsAdmonitionLevelMap
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceMkDocsAdmonitionRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceMkDocsAdmonitionRenderer{
		Config:   html.NewConfig(),
		LevelMap: nil,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs.
func (r *ConfluenceMkDocsAdmonitionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(parser.KindAdmonition, r.renderMkDocsAdmonition)
}

// Define MkDocsAdmonitionType enum
type MkDocsAdmonitionType int

const (
	AInfo MkDocsAdmonitionType = iota
	ANote
	AWarn
	ATip
	ANone
)

func (t MkDocsAdmonitionType) String() string {
	return []string{"info", "note", "warning", "tip", "none"}[t]
}

type MkDocsAdmonitionLevelMap map[ast.Node]int

func (m MkDocsAdmonitionLevelMap) Level(node ast.Node) int {
	return m[node]
}

func ParseMkDocsAdmonitionType(node ast.Node) MkDocsAdmonitionType {
	n, ok := node.(*parser.Admonition)
	if !ok {
		return ANone
	}

	switch string(n.AdmonitionClass) {
	case "info":
		return AInfo
	case "note":
		return ANote
	case "warning":
		return AWarn
	case "tip":
		return ATip
	default:
		return ANone
	}
}

// GenerateMkDocsAdmonitionLevel walks a given node and returns a map of blockquote levels
func GenerateMkDocsAdmonitionLevel(someNode ast.Node) MkDocsAdmonitionLevelMap {

	// We define state variable that tracks BlockQuote level while we walk the tree
	admonitionLevel := 0
	AdmonitionLevelMap := make(map[ast.Node]int)

	rootNode := someNode
	for rootNode.Parent() != nil {
		rootNode = rootNode.Parent()
	}
	_ = ast.Walk(rootNode, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if node.Kind() == ast.KindBlockquote && entering {
			AdmonitionLevelMap[node] = admonitionLevel
			admonitionLevel += 1
		}
		if node.Kind() == ast.KindBlockquote && !entering {
			admonitionLevel -= 1
		}
		return ast.WalkContinue, nil
	})
	return AdmonitionLevelMap
}

// renderBlockQuote will render a BlockQuote
func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmonition(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	//	Initialize BlockQuote level map
	n := node.(*parser.Admonition)
	if r.LevelMap == nil {
		r.LevelMap = GenerateMkDocsAdmonitionLevel(node)
	}

	admonitionType := ParseMkDocsAdmonitionType(node)
	admonitionLevel := r.LevelMap.Level(node)

	if admonitionLevel == 0 && entering && admonitionType != ANone {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", admonitionType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		if string(n.Title) != "" {
			titleHTML := fmt.Sprintf("<p><strong>%s</strong></p>\n", string(n.Title))
			if _, err := writer.Write([]byte(titleHTML)); err != nil {
				return ast.WalkStop, err
			}
		}

		return ast.WalkContinue, nil
	}
	if admonitionLevel == 0 && !entering && admonitionType != ANone {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	return r.renderMkDocsAdmon(writer, source, node, entering)
}

func (r *ConfluenceMkDocsAdmonitionRenderer) renderMkDocsAdmon(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*parser.Admonition)
	if entering {
		if n.Attributes() != nil {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, n, MkDocsAdmonitionAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
