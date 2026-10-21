package transformer

import (
	"io"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// goldmark keeps a node's text as a text.Value: a position in the source, or
// a string the node owns, bound to the decoder that resolves its escapes and
// entities. Value(source) is the decoded text; Str(source) is the text as
// written, which is what the readers in this package expect.

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

// NodeText is a node's text as the link renderer and resolver read it: the
// inline content as written, escapes and entities included, with a newline
// where a soft line break is and nothing where a hard one is. A code span's
// text is the source between its backticks with its newlines kept, where its
// value would turn them into spaces.
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
		writeCodeSpanText(b, n, source)
		return
	case *ast.AutoLink:
		b.WriteString(n.Label.Str(source))
		return
	case *String:
		// mark's own node for text it built itself, such as an <img>'s alt
		// text.
		b.Write(n.Value)
		return
	}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeNodeText(b, child, source)

		if t, ok := child.(*ast.Text); ok && t.SoftLineBreak() {
			b.WriteByte('\n')
		}
	}
}

// writeCodeSpanText writes a code span's text: the source of each line the
// span covers, newlines included, with one space or newline taken off each end
// when both ends have one and the span is not blank.
func writeCodeSpanText(b *strings.Builder, n *ast.CodeSpan, source []byte) {
	if n.Value.IsOwned() {
		b.WriteString(n.Value.Str(source))
		return
	}

	var raw []byte
	for _, index := range n.Value.Indices() {
		raw = append(raw, source[index.Start:index.Stop]...)
	}

	if len(raw) > 1 && !util.IsBlank(raw) &&
		isSpaceOrNewline(raw[0]) && isSpaceOrNewline(raw[len(raw)-1]) {
		raw = raw[1 : len(raw)-1]
	}

	b.Write(raw)
}

func isSpaceOrNewline(c byte) bool {
	return c == ' ' || c == '\n'
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
