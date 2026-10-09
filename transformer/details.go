package transformer

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
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
		case *ast.HTMLBlock, *ast.RawHTML, *ast.Text, *String:
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
				parent.RemoveChild(item.node)
				continue
			}
			parent.InsertBefore(item.node, newReplacementNode(item.newB))
			parent.RemoveChild(item.node)
		}
	}
}

// transformDetailsAt converts the <details> tags in one AST node's raw content,
// advancing *depth by the fragment's net nesting change.
//
// CDATA sections are lifted out first and put back at the end. The HTML
// tokenizer has no notion of CDATA: it reads "<![CDATA[" as a bogus comment
// ending at the first ">", and everything after that as ordinary markup. So a code sample
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
// is tokenized. Letters and digits only, so that neither the tokenizer nor the
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

	balance, lowest := detailsBalance(rawContent)

	// A closing tag with nothing open is stray markup, not part of a split
	// element. Leave it untouched so we never invent an unmatched macro end.
	// Decided on the lowest depth reached rather than on the net change: a
	// fragment that closes one element and opens another nets to zero while
	// closing something all the same.
	if *depth+lowest < 0 {
		return rawContent, false
	}

	// Every fragment is rewritten token by token, balanced or not -- a blank
	// line in a body splits the element across sibling fragments, each one
	// unbalanced -- and every
	// token that is not part of a <details> or its <summary> is passed through
	// byte for byte.
	//
	// The balanced ones used to be run through html.Parse and rendered back.
	// That was never right for storage format: HTML5 ignores "/>" on anything
	// but a void element, so <ri:page .../>, <ac:emoticon .../>,
	// <ri:attachment .../> and the like were read as open elements and the
	// siblings after them moved inside -- a link lost its body to the page it
	// named, an emoticon swallowed the rest of its paragraph -- and tables
	// gained a <tbody> the author never wrote. The result stayed well-formed,
	// so nothing downstream objected and the wrong page was published in
	// silence. Converting three tags needs no document tree.
	//
	// The one thing html.Parse did that was needed is close the end tags HTML
	// lets an author omit: `<td>a<td>b</table>` copied as written is not XML,
	// and Confluence refuses the whole page. That is done by a token pass that
	// only ever inserts an end tag, so storage-format markup is still left as
	// written.
	out, changed := rewriteDetails(rawContent)
	if changed {
		*depth += balance
		out, _ = closeOmittedEndTags(out)
	}

	return out, changed
}

func isVoidElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}
