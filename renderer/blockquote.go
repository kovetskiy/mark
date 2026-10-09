package renderer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceBlockQuoteRenderer struct {
	htmlOptions

	LevelMap BlockQuoteLevelMap
}

// NewConfluenceBlockQuoteRenderer creates a new instance of the ConfluenceBlockQuoteRenderer.
func NewConfluenceBlockQuoteRenderer(opts ...html.Option) html.Extension {
	r := &ConfluenceBlockQuoteRenderer{
		LevelMap: nil,
	}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceBlockQuoteRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindBlockquote: nodeRenderer(r.renderBlockQuote),
	})}
}

type BlockQuoteLevelMap map[ast.Node]int

func (m BlockQuoteLevelMap) Level(node ast.Node) int {
	return m[node]
}

type BlockQuoteClassifier struct {
	patternMap map[string]*regexp.Regexp
}

// blockQuoteMarker matches a line that opens with an admonition marker.
//
// Anchored, and requiring the marker to end on a word boundary, because the
// unanchored version this replaces matched the word anywhere in the line:
// "Information about pricing is below." became an info macro and "Please note
// that the API changed." became a note, since `info` and `note` are substrings
// of both. Four of the commonest words in English documentation silently
// rewrote the paragraphs that used them, and the author's only recourse was to
// reword the sentence.
//
// Everything the documented syntax allows still matches -- "Info: text",
// "**Note:** text", "NOTES:", a bare "Warn" -- because the marker is written
// at the front of the line in every one of them. Emphasis is not in the text
// this sees, goldmark having already made a node of it, so "**Warn**" arrives
// as "Warn".
var blockQuoteMarker = regexp.MustCompile(
	`^(?i)(info|infos|note|notes|warn|warns|warning|warnings|tip|tips)\b[[:punct:]]*(\s|$)`,
)

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

// markerOf returns the admonition marker a line opens with, or "" if the line
// is ordinary prose.
//
// The comment form is stripped first: "<!-- Info -->" is a marker written in a
// way that keeps it out of the rendered page, and testdata/quotes.md has
// carried one since long before this was written.
func markerOf(literal string) string {
	marker := strings.TrimSpace(literal)
	marker = strings.TrimPrefix(marker, "<!--")
	marker = strings.TrimSuffix(marker, "-->")
	marker = strings.TrimSpace(marker)

	found := blockQuoteMarker.FindString(marker)
	if found == "" {
		return ""
	}

	return found
}

// ClassifyingBlockQuote compares a string against a set of patterns and returns its AdmonitionType
// Note: GitHub Alerts ([!NOTE], [!TIP], etc.) are now handled by the superior transformer approach
// in the GitHub Alerts extension, not by this legacy blockquote renderer
func (classifier BlockQuoteClassifier) ClassifyingBlockQuote(literal string) AdmonitionType {

	marker := markerOf(literal)
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

// ParseBlockQuoteType parses the first line of a blockquote and returns its type
// Note: This legacy function only handles traditional "info:", "note:", etc. syntax
// GitHub Alerts ([!NOTE], [!TIP], etc.) are handled by the GitHub Alerts transformer
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
				for _, line := range n.Value.Segments() {
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

// renderBlockQuote will render a BlockQuote
func (r *ConfluenceBlockQuoteRenderer) renderBlockQuote(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	// Initialize BlockQuote level map
	if r.LevelMap == nil {
		r.LevelMap = GenerateBlockQuoteLevel(node)
	}

	quoteType := ParseBlockQuoteType(node, source)
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering && quoteType != AdmonitionNone {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", quoteType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	if quoteLevel == 0 && !entering && quoteType != AdmonitionNone {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	if entering {
		if node.Attributes() != nil {
			_, _ = writer.WriteString("<blockquote")
			html.RenderAttributes(writer, source, node, html.BlockquoteAttributeFilter, rc)
			_ = writer.WriteByte('>')
		} else {
			_, _ = writer.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = writer.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}
