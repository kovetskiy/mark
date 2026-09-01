package transformer

import (
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// AnchorTransformer points same-page links at the heading ids mark actually
// generates.
//
// The two ends of a link are produced by different conventions and never met.
// A heading becomes an id that keeps its capitals and its punctuation --
// "## My Heading" is "My-Heading" -- while an author writing the link uses the
// lowercase-and-hyphens slug every other Markdown tool would have made, and
// writes "#my-heading". Confluence then has a heading with one id and a link
// pointing at another, so the link silently goes nowhere.
//
// Nothing announces this. The page renders, the link is clickable, and it does
// nothing when clicked, which is the sort of fault nobody reports twice -- they
// stop linking to headings instead.
type AnchorTransformer struct{}

// NewAnchorTransformer creates a new AnchorTransformer instance.
func NewAnchorTransformer() *AnchorTransformer {
	return &AnchorTransformer{}
}

// anchorKey reduces an id or a link target to what the two conventions agree
// on: the letters and digits, in order, folded to lower case.
//
// Everything else is discarded rather than mapped, because the conventions do
// not merely punctuate differently -- they disagree about which characters
// survive at all. mark keeps "/" and "." in an id where a slug drops them, so
// "API/v2 Guide" becomes "API/v2-Guide" one way and "apiv2-guide" the other.
// Comparing only the alphanumerics is what makes those the same heading.
//
// "Letters and digits" has to mean it in every script, not just in ASCII: mark
// keeps non-ASCII letters in an id, so an a-z test folded every heading written
// in CJK or Cyrillic down to the empty key -- which is discarded -- and no link
// to one could ever be matched.
func anchorKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Transform implements the parser.ASTTransformer interface.
func (t *AnchorTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	headings := map[string]string{}
	ambiguous := map[string]bool{}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}

		id, ok := node.AttributeString("id")
		if !ok {
			return ast.WalkContinue, nil
		}

		value := attributeString(id)
		key := anchorKey(value)
		if key == "" {
			return ast.WalkContinue, nil
		}

		// Two headings that differ only in punctuation collapse to one key.
		// Rewriting either would be a guess, so both are left alone -- an
		// anchor that does not work is better than one that silently goes to
		// the wrong section.
		if existing, seen := headings[key]; seen && existing != value {
			ambiguous[key] = true
		}
		headings[key] = value

		return ast.WalkContinue, nil
	})

	if len(headings) == 0 {
		return
	}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		link, ok := node.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}

		target, found := strings.CutPrefix(string(link.Destination), "#")
		if !found || target == "" {
			return ast.WalkContinue, nil
		}

		// An id written out exactly -- including one the author set with
		// {#custom-id} -- is already correct and must not be touched.
		for _, id := range headings {
			if id == target {
				return ast.WalkContinue, nil
			}
		}

		key := anchorKey(target)
		if ambiguous[key] {
			return ast.WalkContinue, nil
		}

		if id, ok := headings[key]; ok {
			link.Destination = []byte("#" + id)
		}

		return ast.WalkContinue, nil
	})

	markTargetedHeadings(doc)
}

// AnchorAttribute names the heading attribute that carries the anchor a link
// on this page points at.
//
// Confluence keeps no id on a heading -- it generates its own from the
// element's text -- so a heading has to say where it is with the Anchor macro,
// which is what mark already does for footnotes. The macro is only worth
// emitting for a heading something actually links to: every heading carrying
// one would be markup nobody reads, on every page.
//
// Not an HTML attribute name, so goldmark's HeadingAttributeFilter drops it
// from the rendered tag rather than publishing it.
const AnchorAttribute = "mark:anchor"

// markTargetedHeadings records, on each heading, whether a link on this page
// points at it.
func markTargetedHeadings(doc *ast.Document) {
	targets := map[string]bool{}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if link, ok := node.(*ast.Link); ok {
			if target, found := strings.CutPrefix(string(link.Destination), "#"); found && target != "" {
				targets[target] = true
			}
		}

		return ast.WalkContinue, nil
	})

	if len(targets) == 0 {
		return
	}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}

		id, ok := node.AttributeString("id")
		if !ok {
			return ast.WalkContinue, nil
		}

		value := attributeString(id)
		if targets[value] {
			node.SetAttributeString(AnchorAttribute, []byte(value))
		}

		return ast.WalkContinue, nil
	})
}

// attributeString renders a node attribute value, which goldmark hands back as
// either bytes or a string depending on how it was set.
func attributeString(value any) string {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return ""
	}
}
