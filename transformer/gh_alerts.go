package transformer

import (
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
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
		t.transformBlockquote(blockquote, alertType, reader.Source())

		return ast.WalkContinue, nil
	})
}

// extractAlertType checks if the blockquote starts with GitHub Alert syntax and returns the alert type
func (t *GHAlertsTransformer) extractAlertType(blockquote *ast.Blockquote, reader text.Reader) string {
	paragraph, ok := blockquote.FirstChild().(*ast.Paragraph)
	if !ok {
		return ""
	}

	marker, _ := alertMarker(paragraph, reader.Source())

	// We follow GitHub's strict syntax here and don't allow whitespace between
	// brackets and exclamation mark (e.g., [! NOTE] is not recognized).
	if !strings.HasPrefix(marker, "[!") || !strings.HasSuffix(marker, "]") {
		return ""
	}

	alertType := strings.ToLower(marker[len("[!") : len(marker)-len("]")])
	switch alertType {
	case "note", "tip", "important", "warning", "caution":
		return alertType
	}

	return ""
}

// alertMarker returns the text of the leading Text nodes of paragraph up to
// the end of its first line, and how many nodes that is, or "" if anything but
// text comes first. How goldmark splits "[!NOTE]" into Text nodes is its own
// business, and differs between releases, so the nodes are read as one string.
func alertMarker(paragraph *ast.Paragraph, source []byte) (string, int) {
	var marker strings.Builder

	count := 0
	for node := paragraph.FirstChild(); node != nil; node = node.NextSibling() {
		textNode, ok := node.(*ast.Text)
		if !ok {
			return "", 0
		}

		marker.WriteString(textNode.Value.Str(source))
		count++

		if textNode.SoftLineBreak() || textNode.HardLineBreak() {
			break
		}
	}

	return marker.String(), count
}

// transformBlockquote modifies the blockquote to remove the GitHub Alert syntax
// and adds metadata for rendering
func (t *GHAlertsTransformer) transformBlockquote(blockquote *ast.Blockquote, alertType string, source []byte) {
	// Set a custom attribute to identify this as a GitHub Alert
	SetAttributeText(blockquote, "gh-alert-type", alertType)

	// Find and remove the GitHub Alert syntax from the first paragraph
	firstChild := blockquote.FirstChild()
	if firstChild != nil && firstChild.Kind() == ast.KindParagraph {
		paragraph := firstChild.(*ast.Paragraph)
		t.stripAlertMarker(blockquote, paragraph, source)
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
func (t *GHAlertsTransformer) stripAlertMarker(blockquote *ast.Blockquote, paragraph *ast.Paragraph, source []byte) {
	_, count := alertMarker(paragraph, source)

	currentNode := paragraph.FirstChild()
	for i := 0; i < count && currentNode != nil; i++ {
		next := currentNode.NextSibling()
		paragraph.RemoveChild(currentNode)
		currentNode = next
	}

	// If the original paragraph is now empty, remove it
	if paragraph.FirstChild() == nil {
		blockquote.RemoveChild(paragraph)
	}
}
