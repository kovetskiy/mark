package transformer

import (
	"bytes"
	"text/template"

	"github.com/kovetskiy/mark/v16/macro"
	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// MacroTransformer extracts macro directives from HTML comment blocks in the Goldmark AST,
// removes the definition nodes, and expands matching document text nodes into sub-ASTs.
type MacroTransformer struct {
	BaseDir     string
	IncludePath string
	Templates   *template.Template
	Err         error
}

// NewMacroTransformer creates a new MacroTransformer instance.
func NewMacroTransformer(baseDir string, includePath string, tmpl *template.Template) *MacroTransformer {
	if tmpl == nil {
		tmpl = template.New("stdlib")
	}
	return &MacroTransformer{
		BaseDir:     baseDir,
		IncludePath: includePath,
		Templates:   tmpl,
	}
}

// GetError returns any error encountered during AST transformation.
func (t *MacroTransformer) GetError() error {
	return t.Err
}

// Transform implements the parser.ASTTransformer interface.
func (t *MacroTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	t.TransformWithModified(doc, reader, pc)
}

// TransformWithModified transforms the AST and returns true if any modifications were made.
func (t *MacroTransformer) TransformWithModified(doc *ast.Document, reader text.Reader, pc parser.Context) bool {
	type macroTarget struct {
		startNode      ast.Node
		nodesToRemove  []ast.Node
		fullRawContent []byte
	}

	var targets []macroTarget
	visited := make(map[ast.Node]bool)

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || visited[node] {
			return ast.WalkContinue, nil
		}

		rawContent := extractNodeRawContent(node, reader.Source())

		dir, _ := macro.ParseMacroDirective(rawContent)
		if dir != nil {
			target := macroTarget{
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

	modified := false
	var extractedMacros []macro.Macro

	for _, target := range targets {
		macros, _, err := macro.ExtractMacros(t.BaseDir, t.IncludePath, target.fullRawContent, t.Templates)
		if err != nil {
			t.Err = err
			log.Error().Err(err).Msg("unable to extract macro")
			return false
		}
		if len(macros) == 0 {
			continue
		}
		extractedMacros = append(extractedMacros, macros...)

		for _, n := range target.nodesToRemove {
			if n.Parent() != nil {
				n.Parent().RemoveChild(n.Parent(), n)
			}
		}

		modified = true
	}

	for _, m := range extractedMacros {
		var textNodesToReplace []struct {
			node ast.Node
			val  []byte
		}

		_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}

			switch node.(type) {
			case *ast.Paragraph, *ast.Text:
			default:
				return ast.WalkContinue, nil
			}

			raw := extractNodeRawContent(node, reader.Source())
			if len(raw) > 0 && m.Regexp.Match(raw) {
				textNodesToReplace = append(textNodesToReplace, struct {
					node ast.Node
					val  []byte
				}{node: node, val: raw})
			}
			return ast.WalkContinue, nil
		})

		for _, item := range textNodesToReplace {
			if item.node.Parent() == nil {
				continue
			}

			expanded, err := m.Apply(item.val)
			if err != nil {
				t.Err = err
				log.Error().Err(err).Msg("unable to apply macro")
				return false
			}
			if bytes.Equal(expanded, item.val) {
				continue
			}

			p := goldmark.DefaultParser()
			subDoc := p.Parse(text.NewReader(expanded))
			convertSegmentsToStrings(subDoc, expanded)

			parent := item.node.Parent()
			if parent == nil {
				continue
			}

			for subDoc.FirstChild() != nil {
				child := subDoc.FirstChild()
				subDoc.RemoveChild(subDoc, child)
				parent.InsertBefore(parent, item.node, child)
			}

			if item.node.Parent() != nil {
				item.node.Parent().RemoveChild(item.node.Parent(), item.node)
			}

			modified = true
		}
	}

	return modified
}
