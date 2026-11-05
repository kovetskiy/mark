package renderer

import (
	"strings"

	"github.com/kovetskiy/mark/v17/internal/htmlblock"
	"github.com/kovetskiy/mark/v17/internal/syntax"
	ctransformer "github.com/kovetskiy/mark/v17/transformer"
	"github.com/yuin/goldmark/v2/ast"
)

type BlockQuoteLevelMap map[ast.Node]int

func (m BlockQuoteLevelMap) Level(node ast.Node) int {
	return m[node]
}

// colonAdmonitionTypes maps the word of a colon marker, as
// syntax.AdmonitionWord gives it, to its macro. Note is the info macro, as
// GitHub's [!NOTE] is, so that one word has one colour.
var colonAdmonitionTypes = map[string]AdmonitionType{
	"info":    AdmonitionInfo,
	"note":    AdmonitionInfo,
	"warn":    AdmonitionWarning,
	"warning": AdmonitionWarning,
	"tip":     AdmonitionTip,
}

// ClassifyBlockQuote returns the AdmonitionType of a blockquote's first line
// in the colon syntax: an admonition word and a colon, "Info: text", or a
// comment holding only the word. GitHub Alerts ([!NOTE], [!TIP], etc.) are
// handled by the GitHub Alerts transformer instead.
func ClassifyBlockQuote(line string) AdmonitionType {
	if t, ok := colonAdmonitionTypes[syntax.AdmonitionWord(line)]; ok {
		return t
	}
	return AdmonitionNone
}

// ParseBlockQuoteType returns the type of a blockquote in the colon syntax,
// read off its first line: the first line of its opening paragraph or
// heading, emphasis included, or the first line of an opening HTML block.
// GitHub alerts ("> [!NOTE]") are recognised by the GitHub Alerts transformer
// instead.
func ParseBlockQuoteType(node ast.Node, source []byte) AdmonitionType {
	switch first := node.FirstChild().(type) {
	case nil:
		return AdmonitionNone
	case *ast.HTMLBlock:
		lines := htmlblock.Lines(first, source)
		if len(lines) == 0 {
			return AdmonitionNone
		}
		return ClassifyBlockQuote(lines[0].Str(source))
	default:
		switch first.Kind() {
		case ast.KindParagraph, ast.KindHeading, ctransformer.KindTextBlock:
			var line strings.Builder
			firstLine(first, source, &line)
			return ClassifyBlockQuote(line.String())
		}
	}

	return AdmonitionNone
}

// opaque stands in the first line for inline content whose text is not read
// as words -- code, an image's alt text, storage format -- so that a marker
// after it does not open the line.
const opaque = "\uFFFC"

// firstLine writes the text of node's inline content up to its first line
// break, and reports whether it reached one.
func firstLine(node ast.Node, source []byte, line *strings.Builder) bool {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.CodeSpan, *ast.Image:
			line.WriteString(opaque)
		case *ast.Text:
			line.WriteString(n.Value.Str(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				return true
			}
		case *ctransformer.String:
			if n.Code {
				line.WriteString(opaque)
			} else {
				line.Write(n.Value)
			}
		default:
			if firstLine(child, source, line) {
				return true
			}
		}
	}
	return false
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
