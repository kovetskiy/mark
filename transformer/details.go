package transformer

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// DetailsTransformer walks the AST and transforms HTML <details><summary> tags into
// Confluence <ac:structured-macro ac:name="expand"> elements during compilation.
type DetailsTransformer struct{}

// NewDetailsTransformer creates a new instance of DetailsTransformer.
func NewDetailsTransformer() *DetailsTransformer {
	return &DetailsTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *DetailsTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	var nodesToReplace []struct {
		node ast.Node
		newB []byte
	}

	source := reader.Source()

	// Running <details> nesting depth across the whole document. Fragments are
	// visited in source order, so this lets an unbalanced fragment know whether
	// a closing tag it contains actually belongs to an element opened earlier.
	depth := 0

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.HTMLBlock, *ast.RawHTML, *ast.Text, *ast.String:
			// Text inside an inline code span is literal by definition:
			// `<details>` in prose documents the tag, it does not open one.
			if parent := node.Parent(); parent != nil && parent.Kind() == ast.KindCodeSpan {
				return ast.WalkContinue, nil
			}

			raw := ExtractNodeRawContent(n, source)
			if len(raw) == 0 {
				return ast.WalkContinue, nil
			}

			newRaw, transformed := t.transformDetailsAt(raw, &depth)
			if transformed {
				nodesToReplace = append(nodesToReplace, struct {
					node ast.Node
					newB []byte
				}{node: n, newB: newRaw})
			}
		}

		return ast.WalkContinue, nil
	})

	// Unclosed <details> in the source. Close the macros we opened rather than
	// emit unbalanced markup, which Confluence would reject outright.
	if depth > 0 && len(nodesToReplace) > 0 {
		last := &nodesToReplace[len(nodesToReplace)-1]
		last.newB = append(last.newB, bytes.Repeat(
			[]byte(`</ac:rich-text-body></ac:structured-macro>`), depth)...)
	}

	for _, item := range nodesToReplace {
		parent := item.node.Parent()
		if parent != nil {
			textNode := ast.NewText()
			textNode.SetAttribute([]byte("replacement-content"), item.newB)
			parent.InsertBefore(parent, item.node, textNode)
			parent.RemoveChild(parent, item.node)
		}
	}
}

// transformDetails converts a self-contained <details> tree. Kept for the
// existing callers and tests; document-order callers should use
// transformDetailsAt so nesting depth is tracked across fragments.
func (t *DetailsTransformer) transformDetails(rawContent []byte) ([]byte, bool) {
	depth := 0
	return t.transformDetailsAt(rawContent, &depth)
}

// transformDetailsAt converts the <details> tags in one AST node's raw content,
// advancing *depth by the fragment's net nesting change.
func (t *DetailsTransformer) transformDetailsAt(rawContent []byte, depth *int) ([]byte, bool) {
	lower := bytes.ToLower(rawContent)
	hasOpen := bytes.Contains(lower, []byte("<details"))
	hasClose := bytes.Contains(lower, []byte("</details"))
	if !hasOpen && !hasClose {
		return rawContent, false
	}

	balance := detailsBalance(rawContent)

	// A fragment with unmatched <details> tags cannot survive html.Parse, which
	// would auto-close the dangling element and strand the body outside the
	// macro. This happens whenever the body contains a blank line, since that
	// ends the HTML block and splits the element across sibling AST nodes.
	// Rewrite those fragments token-by-token instead.
	if balance != 0 {
		// A closing tag with nothing open is stray markup, not part of a split
		// element. Leave it untouched so we never invent an unmatched macro end.
		if balance < 0 && *depth+balance < 0 {
			return rawContent, false
		}
		out, changed := rewriteUnbalancedDetails(rawContent)
		if changed {
			*depth += balance
		}
		return out, changed
	}

	if !hasOpen {
		return rawContent, false
	}

	doc, err := html.Parse(bytes.NewReader(rawContent))
	if err != nil {
		return rawContent, false
	}

	var hasDetails bool
	var transform func(*html.Node)
	transform = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Details {
			hasDetails = true
			var summaryNode *html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.DataAtom == atom.Summary {
					summaryNode = c
					break
				}
			}

			var summaryText string
			if summaryNode != nil {
				summaryText = strings.TrimSpace(extractTextFromHTMLNode(summaryNode))
			}

			macroNode := &html.Node{
				Type: html.ElementNode,
				Data: "ac:structured-macro",
				Attr: []html.Attribute{
					{Key: "ac:name", Val: "expand"},
				},
			}

			if summaryText != "" {
				paramNode := &html.Node{
					Type: html.ElementNode,
					Data: "ac:parameter",
					Attr: []html.Attribute{
						{Key: "ac:name", Val: "title"},
					},
				}
				paramNode.AppendChild(&html.Node{
					Type: html.TextNode,
					Data: summaryText,
				})
				macroNode.AppendChild(paramNode)
			}

			bodyNode := &html.Node{
				Type: html.ElementNode,
				Data: "ac:rich-text-body",
			}

			var next *html.Node
			for c := n.FirstChild; c != nil; c = next {
				next = c.NextSibling
				if c != summaryNode {
					n.RemoveChild(c)
					bodyNode.AppendChild(c)
				}
			}

			macroNode.AppendChild(bodyNode)

			if n.Parent != nil {
				n.Parent.InsertBefore(macroNode, n)
				n.Parent.RemoveChild(n)
			}

			for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
				transform(c)
			}
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			transform(c)
		}
	}

	transform(doc)

	if !hasDetails {
		return rawContent, false
	}

	var buf bytes.Buffer
	// Walk body children to avoid rendering <html><head><body> tags
	var body *html.Node
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Html {
			for gc := c.FirstChild; gc != nil; gc = gc.NextSibling {
				if gc.Type == html.ElementNode && gc.DataAtom == atom.Body {
					body = gc
					break
				}
			}
		}
	}

	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			renderHTMLNodeTree(&buf, c)
		}
		if buf.Len() > 0 {
			return buf.Bytes(), true
		}
	}

	return rawContent, false
}

func renderHTMLNodeTree(buf *bytes.Buffer, n *html.Node) {
	if n.Type == html.CommentNode && strings.HasPrefix(n.Data, "[CDATA[") {
		buf.WriteString("<!")
		buf.WriteString(n.Data)
		buf.WriteString(">")
		return
	}

	if n.Type == html.ElementNode {
		buf.WriteString("<")
		buf.WriteString(n.Data)
		for _, a := range n.Attr {
			buf.WriteString(" ")
			if a.Namespace != "" {
				buf.WriteString(a.Namespace)
				buf.WriteString(":")
			}
			buf.WriteString(a.Key)
			buf.WriteString(`="`)
			buf.WriteString(html.EscapeString(a.Val))
			buf.WriteString(`"`)
		}

		if n.FirstChild == nil && isVoidElement(n.Data) {
			buf.WriteString(" />")
			return
		}
		buf.WriteString(">")

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderHTMLNodeTree(buf, c)
		}

		buf.WriteString("</")
		buf.WriteString(n.Data)
		buf.WriteString(">")
		return
	}

	_ = html.Render(buf, n)
}

func isVoidElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func extractTextFromHTMLNode(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(extractTextFromHTMLNode(c))
	}
	return sb.String()
}
