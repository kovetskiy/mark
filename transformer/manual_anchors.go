package transformer

import (
	"bytes"
	"html"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// manualAnchor matches an anchor written by hand: an <a> carrying a name or an
// id and no href, which is how a Markdown author marks a spot to link to when
// the spot is not a heading.
//
// Anything with an href is a link and is left alone.
var manualAnchor = regexp.MustCompile(
	`(?i)^<a\s+(?:name|id)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))\s*/?>$`,
)

// manualAnchorClose matches the </a> that closes one.
var manualAnchorClose = regexp.MustCompile(`(?i)^</a\s*>$`)

// ManualAnchorTransformer turns a hand-written anchor into the Anchor macro.
//
// `<a name="details"></a>` is the ordinary way to mark a place in a Markdown
// document that no heading names. Confluence keeps neither the name nor the id
// -- the element survives, stripped of both -- so the anchor was published as
// an empty <a> and nothing could link to it.
//
// The macro is what a link can reach, and is the same one a heading gets, so a
// link written "#details" finds it exactly as it finds a heading.
type ManualAnchorTransformer struct{}

func NewManualAnchorTransformer() *ManualAnchorTransformer {
	return &ManualAnchorTransformer{}
}

// Transform implements parser.ASTTransformer.
func (t *ManualAnchorTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()

	type replacement struct {
		node  ast.Node
		value []byte
	}

	var found []replacement

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		raw, ok := node.(*ast.RawHTML)
		if !ok {
			return ast.WalkContinue, nil
		}

		tag := bytes.TrimSpace(rawHTMLBytes(raw, source))

		if groups := manualAnchor.FindSubmatch(tag); groups != nil {
			name := firstNonEmpty(groups[1:])
			if len(name) > 0 {
				found = append(found, replacement{node: node, value: anchorMacro(string(name))})
			}

			return ast.WalkContinue, nil
		}

		// The closer belongs to the element that is going away. Dropping it is
		// what keeps the body well-formed, since the macro closes itself.
		if manualAnchorClose.Match(tag) && closesAManualAnchor(node, source) {
			found = append(found, replacement{node: node, value: nil})
		}

		return ast.WalkContinue, nil
	})

	for _, item := range found {
		parent := item.node.Parent()
		if parent == nil {
			continue
		}

		if item.value == nil {
			parent.RemoveChild(parent, item.node)

			continue
		}

		macro := ast.NewString(item.value)
		macro.SetCode(true)

		parent.InsertBefore(parent, item.node, macro)
		parent.RemoveChild(parent, item.node)
	}
}

// closesAManualAnchor reports whether the nearest opening tag before this one
// was an anchor being replaced, so that an ordinary link's </a> is left alone.
func closesAManualAnchor(node ast.Node, source []byte) bool {
	for previous := node.PreviousSibling(); previous != nil; previous = previous.PreviousSibling() {
		raw, ok := previous.(*ast.RawHTML)
		if !ok {
			continue
		}

		tag := bytes.TrimSpace(rawHTMLBytes(raw, source))
		if manualAnchorClose.Match(tag) {
			return false
		}

		if bytes.HasPrefix(bytes.ToLower(tag), []byte("<a")) {
			return manualAnchor.Match(tag)
		}
	}

	return false
}

// anchorMacro is the Anchor macro for a name, escaped as the stdlib template
// escapes it -- html.EscapeString is what xmlesc calls. Written here rather
// than rendered from the template because a transformer has no stdlib, and the
// bytes are inserted into the tree rather than into a stream.
func anchorMacro(name string) []byte {
	return []byte(
		`<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">` +
			html.EscapeString(name) +
			`</ac:parameter></ac:structured-macro>`,
	)
}

// rawHTMLBytes joins the segments of a raw HTML node.
func rawHTMLBytes(node *ast.RawHTML, source []byte) []byte {
	var buf bytes.Buffer

	for i := 0; i < node.Segments.Len(); i++ {
		segment := node.Segments.At(i)
		buf.Write(segment.Value(source))
	}

	return buf.Bytes()
}

// firstNonEmpty returns the first group that matched, since the pattern offers
// three ways to quote the value.
func firstNonEmpty(groups [][]byte) []byte {
	for _, group := range groups {
		if len(group) > 0 {
			return group
		}
	}

	return nil
}
