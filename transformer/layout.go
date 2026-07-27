package transformer

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// LayoutTransformer walks the AST and transforms Confluence layout HTML comment tags
// (e.g. <!-- ac:layout -->, <!-- ac:layout-section type:single -->, <!-- ac:layout-cell -->, etc.)
// into Confluence XML elements (<ac:layout>, <ac:layout-section ac:type="...">, etc.) during AST compilation.
type LayoutTransformer struct{}

// NewLayoutTransformer creates a new instance of LayoutTransformer.
func NewLayoutTransformer() *LayoutTransformer {
	return &LayoutTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *LayoutTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	var nodesToReplace []struct {
		node ast.Node
		newB []byte
	}

	source := reader.Source()

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.HTMLBlock, *ast.RawHTML, *ast.Text, *ast.String:
			raw := ExtractNodeRawContent(n, source)
			if len(raw) == 0 {
				return ast.WalkContinue, nil
			}

			newRaw, transformed := t.transformLayoutComments(raw)
			if transformed {
				nodesToReplace = append(nodesToReplace, struct {
					node ast.Node
					newB []byte
				}{node: n, newB: newRaw})
			}
		}

		return ast.WalkContinue, nil
	})

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

func (t *LayoutTransformer) transformLayoutComments(raw []byte) ([]byte, bool) {
	if !bytes.Contains(raw, []byte("<!-- ac:")) {
		return raw, false
	}

	lines := strings.Split(string(raw), "\n")
	var buf bytes.Buffer
	var modified bool

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		var converted string

		switch trimmed {
		case "<!-- ac:layout -->":
			converted = "<ac:layout>"
		case "<!-- ac:layout end -->":
			converted = "</ac:layout>"
		case "<!-- ac:layout-section type:single -->":
			converted = `<ac:layout-section ac:type="single">`
		case "<!-- ac:layout-section type:two_equal -->":
			converted = `<ac:layout-section ac:type="two_equal">`
		case "<!-- ac:layout-section type:two_left_sidebar -->":
			converted = `<ac:layout-section ac:type="two_left_sidebar">`
		case "<!-- ac:layout-section type:two_right_sidebar -->":
			converted = `<ac:layout-section ac:type="two_right_sidebar">`
		case "<!-- ac:layout-section type:three -->":
			converted = `<ac:layout-section ac:type="three">`
		case "<!-- ac:layout-section type:three_with_sidebars -->":
			converted = `<ac:layout-section ac:type="three_with_sidebars">`
		case "<!-- ac:layout-section end -->":
			converted = "</ac:layout-section>"
		case "<!-- ac:layout-cell -->":
			converted = "<ac:layout-cell>"
		case "<!-- ac:layout-cell end -->":
			converted = "</ac:layout-cell>"
		case "<!-- ac:placeholder -->":
			converted = "<ac:placeholder>"
		case "<!-- ac:placeholder end -->":
			converted = "</ac:placeholder>"
		}

		if converted != "" {
			buf.WriteString(converted)
			modified = true
		} else {
			buf.WriteString(line)
		}

		if i < len(lines)-1 {
			buf.WriteString("\n")
		}
	}

	if modified {
		return buf.Bytes(), true
	}

	return raw, false
}
