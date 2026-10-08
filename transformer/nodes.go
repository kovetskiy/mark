package transformer

import (
	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
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
	parent := paragraph.Parent()
	if parent == nil {
		return false
	}

	if description, ok := parent.(*extast.DefinitionDescription); ok {
		return description.IsTight
	}

	if _, ok := parent.(*ast.ListItem); !ok {
		return false
	}

	list, ok := parent.Parent().(*ast.List)

	return ok && list.IsTight
}

// parseSubDocument parses the bytes a macro or an include expanded to, with
// what the document around them was parsed with that its nodes depend on: the
// Confluence tag parser, and the tight blocks v1 parsed by itself.
func parseSubDocument(source []byte) ast.Node {
	return parser.New(
		parser.WithInlineParsers(
			util.Prioritized(cparser.NewConfluenceTagParser(), 99),
		),
		parser.WithASTTransformers(
			util.Prioritized[parser.ASTTransformer](NewTightBlockTransformer(), 0),
		),
	).Parse(source)
}

// TaskCheckBox is the "[ ]" or "[x]" a task list item opens with.
//
// goldmark v1's task list parser left one of these at the head of the item's
// first block, and the task list renderer reads the item's status from there.
// v2 records the status on the list item instead; TaskCheckBoxTransformer puts
// the node back.
type TaskCheckBox struct {
	ast.BaseInline

	// IsChecked says the box was written "[x]".
	IsChecked bool
}

// KindTaskCheckBox is the ast.NodeKind of a TaskCheckBox.
var KindTaskCheckBox = ast.NewNodeKind("MarkTaskCheckBox")

// Kind implements ast.Node.
func (n *TaskCheckBox) Kind() ast.NodeKind {
	return KindTaskCheckBox
}

// Dump implements ast.Node.
func (n *TaskCheckBox) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"Checked": n.IsChecked,
	})
}

// NewTaskCheckBox returns a TaskCheckBox.
func NewTaskCheckBox(checked bool) *TaskCheckBox {
	n := &TaskCheckBox{IsChecked: checked}
	n.Init(n)

	return n
}

// TaskCheckBoxTransformer puts a TaskCheckBox at the head of the first block
// of every task list item, where goldmark v1 parsed one.
type TaskCheckBoxTransformer struct{}

// NewTaskCheckBoxTransformer returns a TaskCheckBoxTransformer.
func NewTaskCheckBoxTransformer() *TaskCheckBoxTransformer {
	return &TaskCheckBoxTransformer{}
}

// Transform implements parser.ASTTransformer.
func (t *TaskCheckBoxTransformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		status, ok := extension.TaskStatusOf(node)
		if !ok {
			return ast.WalkContinue, nil
		}

		block := node.FirstChild()
		if block == nil {
			return ast.WalkContinue, nil
		}

		checkbox := NewTaskCheckBox(status == extension.TaskStatusCompleted)
		if first := block.FirstChild(); first != nil {
			block.InsertBefore(first, checkbox)
		} else {
			block.AppendChild(checkbox)
		}

		return ast.WalkContinue, nil
	})
}

// ShapeTransformers put back the parts of the tree goldmark v1's parser built
// and v2's does not, which the transformers and renderers here were written
// against: tight paragraphs as TextBlocks, a task's checkbox as a node, and
// the footnotes gathered into one list. The first three run before anything
// else looks at the tree, where v1 had already done it while parsing; the last
// runs after everything, at the priority v1's own footnote transformer had.
// Both compile paths register them.
func ShapeTransformers() []util.PrioritizedValue[parser.ASTTransformer] {
	return []util.PrioritizedValue[parser.ASTTransformer]{
		util.Prioritized[parser.ASTTransformer](NewTightBlockTransformer(), 1),
		util.Prioritized[parser.ASTTransformer](NewTaskCheckBoxTransformer(), 2),
		util.Prioritized[parser.ASTTransformer](NewFootnoteListTransformer(), 3),
		util.Prioritized[parser.ASTTransformer](NewFootnoteBacklinkTransformer(), 999),
	}
}
