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

// BlockQuoteClassifier reads the admonition type off the first line of a
// blockquote written in the colon syntax: "> Info: text". It holds no state,
// so its zero value is ready to use.
type BlockQuoteClassifier struct{}

// LegacyBlockQuoteClassifier returns the classifier for the colon syntax.
func LegacyBlockQuoteClassifier() BlockQuoteClassifier {
	return BlockQuoteClassifier{}
}

// ClassifyingBlockQuote returns the AdmonitionType of a blockquote's first
// line: an admonition word and a colon, or a comment holding only the word.
// GitHub Alerts ([!NOTE], [!TIP], etc.) are handled by the GitHub Alerts
// transformer instead.
func (BlockQuoteClassifier) ClassifyingBlockQuote(literal string) AdmonitionType {
	marker := syntax.AdmonitionMarker(literal)
	if marker == "" {
		return AdmonitionNone
	}

	if t, ok := colonAdmonitionTypes[syntax.AdmonitionWord(marker)]; ok {
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
	var classifier BlockQuoteClassifier

	switch first := node.FirstChild().(type) {
	case nil:
		return AdmonitionNone
	case *ast.HTMLBlock:
		lines := htmlblock.Lines(first, source)
		if len(lines) == 0 {
			return AdmonitionNone
		}
		return classifier.ClassifyingBlockQuote(lines[0].Str(source))
	default:
		switch first.Kind() {
		case ast.KindParagraph, ast.KindHeading, ctransformer.KindTextBlock:
			var line strings.Builder
			firstLine(first, source, &line)
			return classifier.ClassifyingBlockQuote(line.String())
		}
	}

	return AdmonitionNone
}

// firstLine writes the text of node's inline content up to its first line
// break, and reports whether it reached one. Code is left out: it is not a
// marker.
func firstLine(node ast.Node, source []byte, line *strings.Builder) bool {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.CodeSpan:
			continue
		case *ast.Text:
			line.WriteString(n.Value.Str(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				return true
			}
		case *ctransformer.String:
			if !n.Code {
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
