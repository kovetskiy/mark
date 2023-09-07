package renderer

import (
	"fmt"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceBlockQuoteRenderer struct {
	html.Config
	LevelMap BlockQuoteLevelMap
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceBlockQuoteRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceBlockQuoteRenderer{
		Config:   html.NewConfig(),
		LevelMap: nil,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceBlockQuoteRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindBlockquote, r.renderBlockQuote)
}

// Define BlockQuoteType enum
type BlockQuoteType int

const (
	Info BlockQuoteType = iota
	Note
	Warn
	None
)

func (t BlockQuoteType) String() string {
	return []string{"info", "note", "warn", "none"}[t]
}

type BlockQuoteLevelMap map[ast.Node]int

func (m BlockQuoteLevelMap) Level(node ast.Node) int {
	return m[node]
}

// ClassifyingBlockQuote compares a string against a set of patterns and returns a BlockQuoteType
func ClassifyingBlockQuote(literal string) BlockQuoteType {
	infoPattern := regexp.MustCompile(`info|Info|INFO`)
	notePattern := regexp.MustCompile(`note|Note|NOTE`)
	warnPattern := regexp.MustCompile(`warn|Warn|WARN`)

	var t = None
	switch {
	case infoPattern.MatchString(literal):
		t = Info
	case notePattern.MatchString(literal):
		t = Note
	case warnPattern.MatchString(literal):
		t = Warn
	}
	return t
}

// ParseBlockQuoteType parses the first line of a blockquote and returns its type
func ParseBlockQuoteType(node ast.Node, source []byte) BlockQuoteType {
	var t = None

	countParagraphs := 0
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {

		if node.Kind() == ast.KindParagraph && entering {
			countParagraphs += 1
		}
		// Type of block quote should be defined on the first blockquote line
		if countParagraphs < 2 && entering {
			if node.Kind() == ast.KindText {
				n := node.(*ast.Text)
				t = ClassifyingBlockQuote(string(n.Text(source)))
				countParagraphs += 1
			}
			if node.Kind() == ast.KindHTMLBlock {

				n := node.(*ast.HTMLBlock)
				for i := 0; i < n.BaseBlock.Lines().Len(); i++ {
					line := n.BaseBlock.Lines().At(i)
					t = ClassifyingBlockQuote(string(line.Value(source)))
					if t != None {
						break
					}
				}
				countParagraphs += 1
			}
		} else if countParagraphs > 1 && entering {
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})

	return t
}

// GenerateBlockQuoteLevel walks a given node and returns a map of blockquote levels
func GenerateBlockQuoteLevel(someNode ast.Node) BlockQuoteLevelMap {

	// We define state variable that track BlockQuote level while we walk the tree
	blockQuoteLevel := 0
	blockQuoteLevelMap := make(map[ast.Node]int)

	rootNode := someNode
	for rootNode.Parent() != nil {
		rootNode = rootNode.Parent()
	}
	_ = ast.Walk(rootNode, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if node.Kind() == ast.KindBlockquote && entering {
			blockQuoteLevelMap[node] = blockQuoteLevel
			blockQuoteLevel += 1
		}
		if node.Kind() == ast.KindBlockquote && !entering {
			blockQuoteLevel -= 1
		}
		return ast.WalkContinue, nil
	})
	return blockQuoteLevelMap
}

// renderBlockQuote will render a BlockQuote
func (r *ConfluenceBlockQuoteRenderer) renderBlockQuote(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	// Initialize BlockQuote level map
	if r.LevelMap == nil {
		r.LevelMap = GenerateBlockQuoteLevel(node)
	}

	quoteType := ParseBlockQuoteType(node, source)
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering && quoteType != None {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", quoteType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	if quoteLevel == 0 && !entering && quoteType != None {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	return r.goldmarkRenderBlockquote(writer, source, node, entering)
}

// goldmarkRenderBlockquote is the default renderBlockquote implementation from https://github.com/yuin/goldmark/blob/9d6f314b99ca23037c93d76f248be7b37de6220a/renderer/html/html.go#L286
func (r *ConfluenceBlockQuoteRenderer) goldmarkRenderBlockquote(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if n.Attributes() != nil {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, n, html.BlockquoteAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
