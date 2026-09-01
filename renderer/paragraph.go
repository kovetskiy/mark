package renderer

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceParagraphRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceParagraphRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceParagraphRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceParagraphRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindParagraph, r.renderParagraph)
}

func (r *ConfluenceParagraphRenderer) renderParagraph(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	unwrapped := unwrapParagraph(n, source)
	if entering {
		if !unwrapped {
			if n.Attributes() != nil {
				_, _ = w.WriteString("<p")
				html.RenderAttributes(w, n, html.ParagraphAttributeFilter)
				_ = w.WriteByte('>')
			} else {
				_, _ = w.WriteString("<p>")
			}
		}
	} else {
		if !unwrapped {
			_, _ = w.WriteString("</p>")
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

// unwrapParagraph reports whether the paragraph is markup the author wrote out
// by hand, and so must be emitted without a <p> wrapper.
//
// Merely *starting* with a raw fragment is not enough, which is what this used
// to test. `<ac:emoticon ac:name="smile"/> and then prose.` is a paragraph with
// a macro at the front, and dropping the wrapper put the prose after it at bare
// block level -- while the same tag written mid-sentence kept its <p>.
//
// Three shapes are the author's own markup rather than prose:
//
//   - one fragment and nothing else, e.g. a macro on its own line;
//   - a fragment that opens an element and a last fragment that closes it,
//     which is how <b>bold</b> arrives here;
//   - one half of a Confluence element the author spread over several blocks,
//     where a <p> would interleave with the element being built.
func unwrapParagraph(n ast.Node, source []byte) bool {
	first, ok := n.FirstChild().(*ast.RawHTML)
	if !ok {
		return false
	}

	if ast.Node(first) == n.LastChild() {
		return true
	}

	firstTag := rawHTMLTag(first, source)

	if last, ok := n.LastChild().(*ast.RawHTML); ok {
		if closesTag(firstTag, rawHTMLTag(last, source)) {
			return true
		}
	}

	return spansConfluenceElement(firstTag)
}

// rawHTMLTag returns the fragment's bytes, which for an inline raw HTML node is
// a single tag.
func rawHTMLTag(n *ast.RawHTML, source []byte) []byte {
	var buf bytes.Buffer
	for i := 0; i < n.Segments.Len(); i++ {
		segment := n.Segments.At(i)
		buf.Write(segment.Value(source))
	}
	return bytes.TrimSpace(buf.Bytes())
}

// closesTag reports whether closing is the closing tag of the element that
// opening opens.
func closesTag(opening []byte, closing []byte) bool {
	if !isOpeningTag(opening) || !bytes.HasPrefix(closing, []byte("</")) {
		return false
	}

	name := tagName(opening)

	return len(name) > 0 && bytes.EqualFold(name, tagName(closing))
}

// spansConfluenceElement reports whether the fragment is the opening or closing
// half of a Confluence element rather than a complete one. A self-closing tag
// is complete, so whatever follows it in the paragraph is ordinary prose.
func spansConfluenceElement(tag []byte) bool {
	for _, prefix := range [][]byte{[]byte("<ac:"), []byte("<ri:"), []byte("</ac:"), []byte("</ri:")} {
		if bytes.HasPrefix(tag, prefix) {
			return !bytes.HasSuffix(tag, []byte("/>"))
		}
	}

	return false
}

func isOpeningTag(tag []byte) bool {
	return len(tag) > 1 && tag[0] == '<' && tag[1] != '/' && !bytes.HasSuffix(tag, []byte("/>"))
}

// tagName returns the element name of an opening or closing tag, empty if the
// bytes are not a tag at all.
func tagName(tag []byte) []byte {
	if len(tag) == 0 || tag[0] != '<' {
		return nil
	}

	rest := tag[1:]
	rest = bytes.TrimPrefix(rest, []byte("/"))

	end := 0
	for end < len(rest) {
		switch rest[end] {
		case ' ', '\t', '\r', '\n', '/', '>':
			return rest[:end]
		}
		end++
	}

	return rest
}
