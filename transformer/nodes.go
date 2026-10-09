package transformer

import (
	"sync"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark/v2/ast"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// String is an inline node that carries its text itself rather than pointing
// into the source.
//
// goldmark v1 had one, ast.String, and the transformers here used it for two
// things: markup that has to reach the page exactly as it stands -- the
// fragments a macro or an include expands to, and the HTML the well-formedness
// pass repaired -- and plain text, such as an <img>'s alt. v2 has no such node,
// and the ones that come closest each bring something with them: a Text node
// is what the <details>, <img> and well-formedness passes look for, and a
// RawHTML is what the paragraph renderer unwraps. This one is seen by exactly
// the passes that saw goldmark's, and rendered the way goldmark rendered it.
type String struct {
	ast.BaseInline

	// Value is the text, already detached from any source.
	Value []byte

	// Code says Value is storage format to be written as it is. Otherwise it
	// is Markdown text, whose escapes and entities are resolved and which is
	// then escaped for the page.
	Code bool
}

// KindString is the ast.NodeKind of a String.
var KindString = ast.NewNodeKind("MarkString")

// Kind implements ast.Node.
func (n *String) Kind() ast.NodeKind {
	return KindString
}

// Dump implements ast.Node.
func (n *String) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"Value": string(n.Value),
		"Code":  n.Code,
	})
}

// NewString returns a String holding value as text.
func NewString(value []byte) *String {
	n := &String{Value: value}
	n.Init(n)

	return n
}

// newVerbatim returns a String holding value as markup.
func newVerbatim(value []byte) *String {
	n := NewString(value)
	n.Code = true

	return n
}

// TextBlock holds the inline content of a paragraph that is not rendered as
// one: the paragraphs of a tight list item and of a tight definition
// description, and the markup an <img> was cut out of.
//
// goldmark v1 parsed those paragraphs as a node of this kind, and everything
// here -- the transformers looking for paragraphs, the paragraph renderer
// deciding on a <p> -- was written against that. v2 keeps them as paragraphs
// and leaves the renderer to ask whether one is tight, so TightBlockTransformer
// puts this node back where v1 had it before anything else looks.
type TextBlock struct {
	ast.BaseBlock
}

// KindTextBlock is the ast.NodeKind of a TextBlock.
var KindTextBlock = ast.NewNodeKind("MarkTextBlock")

// Kind implements ast.Node.
func (n *TextBlock) Kind() ast.NodeKind {
	return KindTextBlock
}

// Dump implements ast.Node.
func (n *TextBlock) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, nil)
}

// NewTextBlock returns an empty TextBlock.
func NewTextBlock() *TextBlock {
	n := &TextBlock{}
	n.Init(n)

	return n
}

// TightBlockTransformer turns the paragraphs of tight lists and tight
// definition descriptions into TextBlocks, as goldmark v1's parser did.
//
// It has to run before every transformer that looks for paragraphs, so it is
// registered ahead of them all.
type TightBlockTransformer struct{}

// NewTightBlockTransformer returns a TightBlockTransformer.
func NewTightBlockTransformer() *TightBlockTransformer {
	return &TightBlockTransformer{}
}

// Transform implements parser.ASTTransformer.
func (t *TightBlockTransformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	var tight []*ast.Paragraph

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if paragraph, ok := node.(*ast.Paragraph); ok && inTightBlock(paragraph) {
			tight = append(tight, paragraph)
		}

		return ast.WalkContinue, nil
	})

	for _, paragraph := range tight {
		block := NewTextBlock()
		block.SetSource(paragraph.Source())
		block.SetBlankPreviousLines(paragraph.HasBlankPreviousLines())
		block.SetPos(paragraph.Pos())

		for child := paragraph.FirstChild(); child != nil; child = paragraph.FirstChild() {
			block.AppendChild(child)
		}

		for _, attribute := range paragraph.Attributes() {
			block.SetAttribute(attribute.Name, attribute.Value)
		}

		paragraph.Parent().ReplaceChild(paragraph, block)
	}
}

// inTightBlock reports whether goldmark v1 would have parsed paragraph as a
// TextBlock: a child of an item of a tight list, or of a tight definition
// description.
func inTightBlock(paragraph *ast.Paragraph) bool {
	if description, ok := paragraph.Parent().(*extast.DefinitionDescription); ok {
		return description.IsTight
	}

	return html.IsInTightBlock(paragraph)
}

// parseSubDocument parses the bytes a macro or an include expanded to, with
// what the document around them was parsed with that its nodes depend on: the
// Confluence tag parser, and the tight blocks v1 parsed by itself.
func parseSubDocument(source []byte) ast.Node {
	return subDocumentParser().Parse(source)
}

// subDocumentParser is built once: a goldmark parser keeps no state from one
// Parse to the next, and neither do the parsers and the transformer it holds.
var subDocumentParser = sync.OnceValue(func() parser.Parser {
	return parser.New(
		parser.WithInlineParsers(
			util.Prioritized(cparser.NewConfluenceTagParser(), 99),
		),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](NewTightBlockTransformer(), 0),
		),
	)
})

// ShapeTransformers put back the parts of the tree goldmark v1's parser built
// and v2's does not, which the transformers and renderers here were written
// against: tight paragraphs as TextBlocks, and the footnotes gathered into one
// list. The first two run before anything else looks at the tree, where v1 had
// already done it while parsing; the last
// runs after everything, at the priority v1's own footnote transformer had.
// Both compile paths register them.
func ShapeTransformers() []util.PrioritizedValue[parser.ASTTransformer] {
	return []util.PrioritizedValue[parser.ASTTransformer]{
		util.Prioritized[parser.ASTTransformer](NewTightBlockTransformer(), 1),
		util.Prioritized[parser.ASTTransformer](NewFootnoteListTransformer(), 3),
		util.Prioritized[parser.ASTTransformer](NewFootnoteBacklinkTransformer(), 999),
	}
}
