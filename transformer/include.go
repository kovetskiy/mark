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

		// Only process Include directives, ignoring Macro definition blocks
		if len(rawContent) > 0 && bytes.Contains(rawContent, []byte("<!--")) && bytes.Contains(rawContent, []byte("Include:")) && !bytes.Contains(rawContent, []byte("Macro:")) {
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
