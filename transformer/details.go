package transformer

import (
	"bytes"
	"fmt"
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

	// Fragments folded into a preceding sibling, so they are not transformed
	// again in their own right.
	absorbed := map[ast.Node]bool{}

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

			// Already folded into an earlier sibling's fragment.
			if absorbed[node] {
				return ast.WalkContinue, nil
			}

			raw := ExtractNodeRawContent(n, source)
			if len(raw) == 0 {
				return ast.WalkContinue, nil
			}

			// Inline HTML arrives one AST node per tag, so "<details>" and its
			// "<summary>s</summary>" land in separate fragments and the title is
			// invisible to the fragment that opens the macro. Fold the following
			// inline siblings in so the element is seen whole, exactly as a block
			// context already sees it.
			var folded []ast.Node
			raw, folded = coalesceInlineDetails(n, raw, source)
			for _, f := range folded {
				absorbed[f] = true
			}

			newRaw, transformed := t.transformDetailsAt(raw, &depth)
			if transformed {
				nodesToReplace = append(nodesToReplace, struct {
					node ast.Node
					newB []byte
				}{node: n, newB: newRaw})
				// The folded siblings' bytes are now part of newB; drop the
				// originals so their content is not emitted twice.
				for _, f := range folded {
					nodesToReplace = append(nodesToReplace, struct {
						node ast.Node
						newB []byte
					}{node: f, newB: nil})
				}
			}
		}

		return ast.WalkContinue, nil
	})

	// Unclosed <details> in the source. Close the macros we opened rather than
	// emit unbalanced markup, which Confluence would reject outright. Folded
	// siblings carry no content, so skip back past them to the fragment that
	// actually holds the rewritten markup.
	if depth > 0 {
		for i := len(nodesToReplace) - 1; i >= 0; i-- {
			if nodesToReplace[i].newB == nil {
				continue
			}
			nodesToReplace[i].newB = append(nodesToReplace[i].newB, bytes.Repeat(
				[]byte(`</ac:rich-text-body></ac:structured-macro>`), depth)...)
			break
		}
	}

	for _, item := range nodesToReplace {
		parent := item.node.Parent()
		if parent != nil {
			// A folded sibling's bytes moved into the fragment that opened the
			// macro; remove it outright so nothing is emitted twice.
			if item.newB == nil {
				parent.RemoveChild(parent, item.node)
				continue
			}
			textNode := ast.NewText()
			textNode.SetAttribute([]byte("replacement-content"), item.newB)
			parent.InsertBefore(parent, item.node, textNode)
			parent.RemoveChild(parent, item.node)
		}
	}
}

// transformDetailsAt converts the <details> tags in one AST node's raw content,
// advancing *depth by the fragment's net nesting change.
//
// CDATA sections are lifted out first and put back at the end. html.Parse has
// no notion of CDATA: it reads "<![CDATA[" as a bogus comment ending at the
// first ">", and everything after that as ordinary markup. So a code sample
// containing ">" was cut in two -- the tail re-parsed and rewritten, the
// section left unterminated -- which is the one thing CDATA is there to
// prevent, and it failed the page outright with "unexpected EOF in CDATA
// section".
func (t *DetailsTransformer) transformDetailsAt(rawContent []byte, depth *int) ([]byte, bool) {
	protected, sections := protectCDATA(rawContent)

	out, changed := t.transformDetailsMarkup(protected, depth)
	if !changed {
		return rawContent, false
	}

	return restoreCDATA(out, sections), true
}

// cdataToken is what a CDATA section is stood in by while the markup around it
// is parsed. Letters and digits only, so that neither html.Parse nor the
// renderer's escaping can alter it.
const cdataToken = "MARKCDATASECTION"

// protectCDATA replaces every CDATA section with a token and returns the
// sections in the order they were found.
func protectCDATA(raw []byte) ([]byte, [][]byte) {
	if !bytes.Contains(raw, cdataOpen) {
		return raw, nil
	}

	// A token the document is not already using, so that restoring cannot put a
	// section somewhere the author wrote the token themselves.
	token := cdataToken
	for bytes.Contains(raw, []byte(token)) {
		token += "X"
	}

	var (
		buf      bytes.Buffer
		sections [][]byte
		rest     = raw
	)

	for len(rest) > 0 {
		start := bytes.Index(rest, cdataOpen)
		if start == -1 {
			buf.Write(rest)

			break
		}

		buf.Write(rest[:start])

		end := bytes.Index(rest[start:], cdataClose)
		if end == -1 {
			// Unterminated. Everything left is inside the section as far as
			// anything downstream can tell, so it is carried out whole.
			sections = append(sections, rest[start:])
			fmt.Fprintf(&buf, "%s%d%s", token, len(sections)-1, token)

			break
		}

		stop := start + end + len(cdataClose)
		sections = append(sections, rest[start:stop])
		fmt.Fprintf(&buf, "%s%d%s", token, len(sections)-1, token)

		rest = rest[stop:]
	}

	if len(sections) == 0 {
		return raw, nil
	}

	// The token is handed back with the sections so that restoring uses the one
	// substituting actually chose.
	return buf.Bytes(), append([][]byte{[]byte(token)}, sections...)
}

// restoreCDATA puts the sections back where their tokens stand.
func restoreCDATA(rendered []byte, sections [][]byte) []byte {
	if len(sections) == 0 {
		return rendered
	}

	token := string(sections[0])
	for i, section := range sections[1:] {
		rendered = bytes.ReplaceAll(
			rendered, fmt.Appendf(nil, "%s%d%s", token, i, token), section,
		)
	}

	return rendered
}

// transformDetailsMarkup does the work on content holding no CDATA section.
func (t *DetailsTransformer) transformDetailsMarkup(rawContent []byte, depth *int) ([]byte, bool) {
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
