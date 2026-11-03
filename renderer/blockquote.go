package renderer

import (
	"regexp"

	"github.com/kovetskiy/mark/v16/internal/htmlblock"
	"github.com/kovetskiy/mark/v16/internal/syntax"
	"github.com/yuin/goldmark/v2/ast"
)

type BlockQuoteLevelMap map[ast.Node]int

func (m BlockQuoteLevelMap) Level(node ast.Node) int {
	return m[node]
}

type BlockQuoteClassifier struct {
	patternMap map[string]*regexp.Regexp
}

func LegacyBlockQuoteClassifier() BlockQuoteClassifier {
	return BlockQuoteClassifier{
		patternMap: map[string]*regexp.Regexp{
			"info": regexp.MustCompile(`(?i)^info`),
			"note": regexp.MustCompile(`(?i)^note`),
			"warn": regexp.MustCompile(`(?i)^warn`),
			"tip":  regexp.MustCompile(`(?i)^tip`),
		},
	}
}

// ClassifyingBlockQuote compares a string against a set of patterns and returns its AdmonitionType.
// GitHub Alerts ([!NOTE], [!TIP], etc.) are handled by the GitHub Alerts transformer instead.
func (classifier BlockQuoteClassifier) ClassifyingBlockQuote(literal string) AdmonitionType {

	marker := syntax.AdmonitionMarker(literal)
	if marker == "" {
		return AdmonitionNone
	}

	var t = AdmonitionNone
	switch {
	case classifier.patternMap["info"].MatchString(marker):
		t = AdmonitionInfo
	case classifier.patternMap["note"].MatchString(marker):
		t = AdmonitionNote
	case classifier.patternMap["warn"].MatchString(marker):
		t = AdmonitionWarning
	case classifier.patternMap["tip"].MatchString(marker):
		t = AdmonitionTip
	}
	return t
}

// ParseBlockQuoteType parses the first line of a blockquote and returns its
// type in the "> Info:" syntax. GitHub alerts ("> [!NOTE]") are recognised by
// the GitHub Alerts transformer instead.
func ParseBlockQuoteType(node ast.Node, source []byte) AdmonitionType {
	var t = AdmonitionNone
	var legacyClassifier = LegacyBlockQuoteClassifier()

	countParagraphs := 0
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {

		if node.Kind() == ast.KindParagraph && entering {
			countParagraphs += 1
		}
		// Type of block quote should be defined on the first blockquote line
		if countParagraphs < 2 && entering {
			if node.Kind() == ast.KindText {
				n := node.(*ast.Text)
				t = legacyClassifier.ClassifyingBlockQuote(n.Value.Str(source))
				countParagraphs += 1
			}
			if node.Kind() == ast.KindHTMLBlock {

				n := node.(*ast.HTMLBlock)
				for _, line := range htmlblock.Lines(n, source) {
					t = legacyClassifier.ClassifyingBlockQuote(line.Str(source))
					if t != AdmonitionNone {
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
