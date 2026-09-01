package transformer

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// GHAlertsTransformer transforms GitHub Alert syntax ([!NOTE], [!TIP], etc.)
// into a custom AST node that can be rendered as Confluence macros
type GHAlertsTransformer struct{}

// NewGHAlertsTransformer creates a new GitHub Alerts transformer
func NewGHAlertsTransformer() *GHAlertsTransformer {
	return &GHAlertsTransformer{}
}

// Transform implements the parser.ASTTransformer interface
func (t *GHAlertsTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		// Only process blockquote nodes
		blockquote, ok := node.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Check if this blockquote contains GitHub Alert syntax
		alertType := t.extractAlertType(blockquote, reader)
		if alertType == "" {
			return ast.WalkContinue, nil
		}

		// Transform the blockquote into a GitHub Alert node
		t.transformBlockquote(blockquote, alertType)

		return ast.WalkContinue, nil
	})
}

// extractAlertType checks if the blockquote starts with GitHub Alert syntax and returns the alert type
func (t *GHAlertsTransformer) extractAlertType(blockquote *ast.Blockquote, reader text.Reader) string {
	// Look for the first paragraph in the blockquote
	firstChild := blockquote.FirstChild()
	if firstChild == nil || firstChild.Kind() != ast.KindParagraph {
		return ""
	}

	paragraph := firstChild.(*ast.Paragraph)

	// Check if the paragraph starts with the GitHub Alert pattern [!TYPE]
	firstText := paragraph.FirstChild()
	if firstText == nil || firstText.Kind() != ast.KindText {
		return ""
	}

	// Look for the pattern: [!ALERTTYPE]
	// We need to check for three consecutive text nodes: "[", "!ALERTTYPE", "]"
	// This is the intended behavior for GitHub Alerts which should be at the very start.
	// Note: We follow GitHub's strict syntax here and don't allow whitespace between
	// brackets and exclamation mark (e.g., [! NOTE] is not recognized).
	currentNode := firstText
	var nodes []ast.Node

	// Collect up to 3 text nodes
	for i := 0; i < 3 && currentNode != nil && currentNode.Kind() == ast.KindText; i++ {
		nodes = append(nodes, currentNode)
		currentNode = currentNode.NextSibling()
	}

	if len(nodes) < 3 {
		return ""
	}

	leftText := nodes[0].(*ast.Text)
	middleText := nodes[1].(*ast.Text)
	rightText := nodes[2].(*ast.Text)

	leftContent := string(leftText.Segment.Value(reader.Source()))
	middleContent := string(middleText.Segment.Value(reader.Source()))
	rightContent := string(rightText.Segment.Value(reader.Source()))

	// Check for the exact pattern
	if leftContent == "[" && rightContent == "]" && strings.HasPrefix(middleContent, "!") {
		alertType := strings.ToLower(strings.TrimPrefix(middleContent, "!"))

		// Validate it's a recognized GitHub Alert type
		switch alertType {
		case "note", "tip", "important", "warning", "caution":
			return alertType
		}
	}

	return ""
}

// transformBlockquote modifies the blockquote to remove the GitHub Alert syntax
// and adds metadata for rendering
func (t *GHAlertsTransformer) transformBlockquote(blockquote *ast.Blockquote, alertType string) {
	// Set a custom attribute to identify this as a GitHub Alert
	blockquote.SetAttribute([]byte("gh-alert-type"), []byte(alertType))

	// Find and remove the GitHub Alert syntax from the first paragraph
	firstChild := blockquote.FirstChild()
	if firstChild != nil && firstChild.Kind() == ast.KindParagraph {
		paragraph := firstChild.(*ast.Paragraph)
		t.stripAlertMarker(blockquote, paragraph)
	}
}

// stripAlertMarker removes the [!TYPE] syntax, leaving the blockquote holding
// only what the author wrote underneath it.
//
// The alert's title used to be added back here as a paragraph of its own, which
// made it the first line of the macro's body rather than its header. The
// renderer passes it as the macro's title parameter instead, which is what
// Confluence styles as a header -- so nothing has to be synthesised into the
// body at all.
func (t *GHAlertsTransformer) stripAlertMarker(blockquote *ast.Blockquote, paragraph *ast.Paragraph) {
	// Remove the first three nodes ([ !TYPE ]) from the original paragraph
	currentNode := paragraph.FirstChild()
	for i := 0; i < 3 && currentNode != nil; i++ {
		next := currentNode.NextSibling()
		paragraph.RemoveChild(paragraph, currentNode)
		currentNode = next
	}

	// If the original paragraph is now empty, remove it
	if paragraph.FirstChild() == nil {
		blockquote.RemoveChild(blockquote, paragraph)
	}
}
