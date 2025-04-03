package renderer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceBlockQuoteRenderer struct {
	html.Config
	LevelMap       BlockQuoteLevelMap
	BlockQuoteNode ast.Node
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceBlockQuoteRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceBlockQuoteRenderer{
		Config:         html.NewConfig(),
		LevelMap:       nil,
		BlockQuoteNode: nil,
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
	Tip
	None
)

// Define BlockQuoteClassifierType enum
type BlockQuoteClassifierType int

const (
	Legacy BlockQuoteClassifierType = iota
	GHAlerts
)

func (t BlockQuoteType) String() string {
	return []string{"info", "note", "warning", "tip", "none"}[t]
}

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
			"info": regexp.MustCompile(`(?i)info`),
			"note": regexp.MustCompile(`(?i)note`),
			"warn": regexp.MustCompile(`(?i)warn`),
			"tip":  regexp.MustCompile(`(?i)tip`),
		},
	}
}

func GHAlertsBlockQuoteClassifier() BlockQuoteClassifier {
	return BlockQuoteClassifier{
		patternMap: map[string]*regexp.Regexp{
			"info": regexp.MustCompile(`(?i)^\!(note|important)`),
			"note": regexp.MustCompile(`(?i)^\!warning`),
			"warn": regexp.MustCompile(`(?i)^\!caution`),
			"tip":  regexp.MustCompile(`(?i)^\!tip`),
		},
	}
}

// ClassifyingBlockQuote compares a string against a set of patterns and returns a BlockQuoteType
func (classifier BlockQuoteClassifier) ClassifyingBlockQuote(literal string) BlockQuoteType {

	var t = None
	switch {
	case classifier.patternMap["info"].MatchString(literal):
		t = Info
	case classifier.patternMap["note"].MatchString(literal):
		t = Note
	case classifier.patternMap["warn"].MatchString(literal):
		t = Warn
	case classifier.patternMap["tip"].MatchString(literal):
		t = Tip
	}
	return t
}

// ParseBlockQuoteType parses the first line of a blockquote and returns its type
func ParseBlockQuoteType(node ast.Node, source []byte) (BlockQuoteType, BlockQuoteClassifierType, ast.Node) {
	var t = None
	var ct = Legacy
	var context ast.Node = nil
	var legacyClassifier = LegacyBlockQuoteClassifier()
	var ghAlertsClassifier = GHAlertsBlockQuoteClassifier()

	countParagraphs := 0
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {

		if node.Kind() == ast.KindParagraph && entering {
			countParagraphs += 1
		}
		// Type of block quote should be defined on the first blockquote line
		if countParagraphs < 2 && entering {
			if node.Kind() == ast.KindText {
				n := node.(*ast.Text)
				t = legacyClassifier.ClassifyingBlockQuote(string(n.Value(source)))
				// If the node is a text node but classification returned none do not give up!
				// Find the next two sibling nodes midNode and rightNode,
				// 1. If both are also a text node
				// 2. and the original node (node) text value is '['
				// 3. and the rightNode text value is ']'
				// It means with high degree of confidence that the original md doc contains a Github alert type of blockquote
				// Classifying the next text type node (midNode) will confirm that.
				if t == None {
					midNode := node.NextSibling()

					if midNode != nil && midNode.Kind() == ast.KindText {
						rightNode := midNode.NextSibling()
						midTextNode := midNode.(*ast.Text)
						if rightNode != nil && rightNode.Kind() == ast.KindText {
							rightTextNode := rightNode.(*ast.Text)
							if string(n.Value(source)) == "[" && string(rightTextNode.Value(source)) == "]" {
								t = ghAlertsClassifier.ClassifyingBlockQuote(string(midTextNode.Value(source)))
								ct = GHAlerts
								context = midNode
							}
						}
					}
				}
				countParagraphs += 1
			}
			if node.Kind() == ast.KindHTMLBlock {

				n := node.(*ast.HTMLBlock)
				for i := 0; i < n.BaseBlock.Lines().Len(); i++ {
					line := n.BaseBlock.Lines().At(i)
					t = legacyClassifier.ClassifyingBlockQuote(string(line.Value(source)))
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

	return t, ct, context
}

// Helper function to update the source in place
func ghaAlertsUpdateSourceInPlace(source []byte, node ast.Node, value string) {
	leftNode := node.PreviousSibling().(*ast.Text)
	midNode := node.(*ast.Text)
	rightNode := node.NextSibling().(*ast.Text)

	title := string(midNode.Value(source))
	sanitizedTitle := strings.Replace(strings.ToLower(title), "!", "", -1)
	capitalizedValue := strings.ToUpper(sanitizedTitle[:1]) + sanitizedTitle[1:]
	if value != "" {
		capitalizedValue = strings.ToUpper(value[:1]) + value[1:]
	}

	// Modify the source byte slice directly
	copy(source[leftNode.Segment.Start:leftNode.Segment.Stop], []byte(" "))
	format := fmt.Sprintf("%%-%ds", len(title))
	copy(source[midNode.Segment.Start:midNode.Segment.Stop], []byte(fmt.Sprintf(format, capitalizedValue)))
	copy(source[rightNode.Segment.Start:rightNode.Segment.Stop], []byte(" "))
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

	quoteType, classifierType, blockQuoteCtxNode := ParseBlockQuoteType(node, source)
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering && quoteType != None {
		r.BlockQuoteNode = node

		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", quoteType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		if classifierType == GHAlerts {
			// Using quoteType as value like so: ghaAlertsUpdateSourceInPlace(source, blockQuoteCtxNode, quoteType.String())
			// Will result in the blockquote title replaced by the blockquote type
			ghaAlertsUpdateSourceInPlace(source, blockQuoteCtxNode, "")
		}
		return ast.WalkContinue, nil
	}
	if quoteLevel == 0 && !entering && node == r.BlockQuoteNode {
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
