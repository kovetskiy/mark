package transformer

import (
	"bytes"
	"text/template"

	"github.com/kovetskiy/mark/v16/includes"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// IncludeTransformer transforms <!-- Include: ... --> directives in the Goldmark AST
// by reading, expanding, and inserting the parsed AST nodes of included templates.
type IncludeTransformer struct {
	BaseDir     string
	IncludePath string
	Templates   *template.Template
}

// NewIncludeTransformer creates a new IncludeTransformer instance.
func NewIncludeTransformer(baseDir string, includePath string, tmpl *template.Template) *IncludeTransformer {
	if tmpl == nil {
		tmpl = template.New("stdlib")
	}
	return &IncludeTransformer{
		BaseDir:     baseDir,
		IncludePath: includePath,
		Templates:   tmpl,
	}
}

func extractNodeRawContent(node ast.Node, source []byte) []byte {
	switch t := node.(type) {
	case *ast.HTMLBlock:
		return t.Text(source)
	case *ast.RawHTML:
		var buf bytes.Buffer
		for i := 0; i < t.Segments.Len(); i++ {
			seg := t.Segments.At(i)
			buf.Write(seg.Value(source))
		}
		return buf.Bytes()
	case *ast.Text:
		return t.Segment.Value(source)
	}
	return nil
}

func convertSegmentsToStrings(doc ast.Node, source []byte) {
	var nodesToReplace []struct {
		textNode *ast.Text
		val      []byte
	}

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if textNode, ok := n.(*ast.Text); ok {
			val := textNode.Segment.Value(source)
			valCopy := make([]byte, len(val))
			copy(valCopy, val)
			nodesToReplace = append(nodesToReplace, struct {
				textNode *ast.Text
				val      []byte
			}{textNode: textNode, val: valCopy})
		}
		return ast.WalkContinue, nil
	})

	for _, item := range nodesToReplace {
		parent := item.textNode.Parent()
		if parent != nil {
			strNode := ast.NewString(item.val)
			parent.InsertBefore(parent, item.textNode, strNode)
			parent.RemoveChild(parent, item.textNode)
		}
	}
}

// Transform implements the parser.ASTTransformer interface.
func (t *IncludeTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	type includeTarget struct {
		startNode      ast.Node
		nodesToRemove  []ast.Node
		fullRawContent []byte
	}

	var targets []includeTarget
	visited := make(map[ast.Node]bool)

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || visited[node] {
			return ast.WalkContinue, nil
		}

		rawContent := extractNodeRawContent(node, reader.Source())

		if len(rawContent) > 0 && bytes.Contains(rawContent, []byte("<!--")) && bytes.Contains(rawContent, []byte("Include:")) {
			target := includeTarget{
				startNode:      node,
				nodesToRemove:  []ast.Node{node},
				fullRawContent: rawContent,
			}
			visited[node] = true

			if !bytes.Contains(rawContent, []byte("-->")) {
				var combined bytes.Buffer
				combined.Write(rawContent)
				for sibling := node.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
					sibContent := extractNodeRawContent(sibling, reader.Source())
					combined.Write(sibContent)
					target.nodesToRemove = append(target.nodesToRemove, sibling)
					visited[sibling] = true
					if bytes.Contains(sibContent, []byte("-->")) {
						break
					}
				}
				target.fullRawContent = combined.Bytes()
			}

			targets = append(targets, target)
		}

		return ast.WalkContinue, nil
	})

	for _, target := range targets {
		tmpl, expanded, _, err := includes.ProcessIncludes(t.BaseDir, t.IncludePath, target.fullRawContent, t.Templates)
		if err != nil || bytes.Equal(expanded, target.fullRawContent) {
			continue
		}
		t.Templates = tmpl

		p := goldmark.DefaultParser()
		subDoc := p.Parse(text.NewReader(expanded))
		convertSegmentsToStrings(subDoc, expanded)

		parent := target.startNode.Parent()
		if parent == nil {
			continue
		}

		for subDoc.FirstChild() != nil {
			child := subDoc.FirstChild()
			subDoc.RemoveChild(subDoc, child)
			parent.InsertBefore(parent, target.startNode, child)
		}

		for _, n := range target.nodesToRemove {
			if n.Parent() != nil {
				n.Parent().RemoveChild(n.Parent(), n)
			}
		}
	}
}
