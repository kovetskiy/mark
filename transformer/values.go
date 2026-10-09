package transformer

import (
	"io"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
)

// goldmark v2 keeps a node's text as a text.Value: a position in the source,
// or a string the node owns, bound to the decoder that resolves its escapes
// and entities. Value(source) is the decoded text; Str(source) is the text as
// written, which is what goldmark v1 handed back as bytes and what every
// reader in this package was written against.

// markdownDecoder resolves backslash escapes and entity and numeric references
// the way goldmark's parser does, for a value that is Markdown source still to
// be read.
var markdownDecoder = text.NewDecoder()

// SourceValue is a value that holds Markdown source, written out as an author
// would have written it.
func SourceValue(s string) text.SingleLineValue {
	return text.NewSingleLineValueFromString(s, markdownDecoder)
}

// PlainValue is a value that holds the text itself, with nothing left to
// decode: a URL mark built, or an attribute an HTML parser already decoded.
func PlainValue(s string) text.SingleLineValue {
	return text.NewSingleLineValueFromString(s, text.IdentityDecoder)
}

// AttributeText returns a node attribute as it was written, and whether the
// node has it at all.
func AttributeText(node ast.Node, name string, source []byte) (string, bool) {
	value, ok := node.Attribute(name)
	if !ok {
		return "", false
	}

	return value.Str(source), true
}

// SetAttributeText sets an attribute that holds the value itself, such as the
// markup a transformer hands the renderer, rather than Markdown source.
func SetAttributeText(node ast.Node, name, value string) {
	node.SetAttribute(name, text.NewMultiLineValueFromString(value, text.IdentityDecoder))
}

// ReplacementContentAttribute names the attribute a transformer leaves on an
// empty Text node to have the renderer write its markup in the node's place.
const ReplacementContentAttribute = "replacement-content"

// newReplacementNode is the Text node that stands in for markup a transformer
// rewrote.
func newReplacementNode(markup []byte) *ast.Text {
	node := ast.NewText(text.SingleLineValue{})
	SetAttributeText(node, ReplacementContentAttribute, string(markup))

	return node
}

// ownedSingle detaches a value from the source it points into, keeping the
// text as written and the decoder goldmark's parser would have bound to it.
func ownedSingle(v text.SingleLineValue, source []byte) text.SingleLineValue {
	if v.IsOwned() {
		return v
	}

	return SourceValue(strings.Clone(v.Str(source)))
}

// ownedMulti is ownedSingle for a value that may span lines.
func ownedMulti(v text.MultiLineValue, source []byte) text.MultiLineValue {
	if v.IsOwned() {
		return v
	}

	return text.NewMultiLineValueFromString(strings.Clone(v.Str(source)), markdownDecoder)
}

// NodeText is a node's text as goldmark v1's Node.Text gave it, which the link
// renderer and resolver were written against: the inline content as written,
// escapes and entities included, with a newline where a soft line break was.
func NodeText(node ast.Node, source []byte) string {
	var b strings.Builder
	writeNodeText(&b, node, source)

	return b.String()
}

func writeNodeText(b *strings.Builder, node ast.Node, source []byte) {
	switch n := node.(type) {
	case *ast.Text:
		b.WriteString(n.Value.Str(source))
		return
	case *ast.RawHTML:
		b.WriteString(n.Value.Str(source))
		return
	case *ast.CodeSpan:
		b.WriteString(n.Value.Str(source))
		return
	case *ast.AutoLink:
		b.WriteString(n.Label.Str(source))
		return
	}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeNodeText(b, child, source)

		if t, ok := child.(*ast.Text); ok && t.SoftLineBreak() {
			b.WriteByte('\n')
		}
	}
}

// DecodeMarkdown resolves the backslash escapes and the entity and numeric
// references in Markdown text, the way goldmark's parser does for the values
// it builds. The result may share memory with b and must not be written to.
func DecodeMarkdown(b []byte) []byte {
	return markdownDecoder.Decode(b)
}

// DecodeMarkdownTo is DecodeMarkdown written straight to w.
func DecodeMarkdownTo(w io.Writer, b []byte) (int, error) {
	return markdownDecoder.DecodeTo(w, b)
}
