package transformer

import (
	"bytes"
	"text/template"

	"github.com/kovetskiy/mark/v16/macro"
	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// MacroTransformer extracts macro directives from HTML comment blocks in the Goldmark AST,
// removes the definition nodes, and expands matching document text nodes into sub-ASTs.
type MacroTransformer struct {
	FilePath    string
	BaseDir     string
	IncludePath string
	Templates   *template.Template
	Err         error

	// macros accumulates across pipeline passes. A definition is removed from
	// the tree as soon as it is read, so a transformer that rebuilt this list
	// each pass would forget every macro it had already seen -- and the pass
	// that matters is often a later one, after an include has brought in the
	// text a macro was written for.
	macros []macro.Macro
}

// NewMacroTransformer creates a new MacroTransformer instance.
func NewMacroTransformer(filePath string, baseDir string, includePath string, tmpl *template.Template) *MacroTransformer {
	if tmpl == nil {
		tmpl = template.New("stdlib")
	}
	return &MacroTransformer{
		FilePath:    filePath,
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
		lineNum        int
	}

	var targets []macroTarget
	visited := make(map[ast.Node]bool)

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || visited[node] {
			return ast.WalkContinue, nil
		}

		rawContent := ExtractNodeRawContent(node, reader.Source())

		dir, _ := macro.ParseMacroDirective(rawContent)
		if dir != nil {
			target := macroTarget{
				startNode:      node,
				nodesToRemove:  []ast.Node{node},
				fullRawContent: rawContent,
				lineNum:        getNodeLineNumber(node, reader.Source()),
			}
			visited[node] = true

			if !bytes.Contains(rawContent, []byte("-->")) {
				var combined bytes.Buffer
				combined.Write(rawContent)
				for sibling := node.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
					sibContent := ExtractNodeRawContent(sibling, reader.Source())
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

	for _, target := range targets {
		macros, _, err := macro.ExtractMacros(t.BaseDir, t.IncludePath, target.fullRawContent, t.Templates)
		if err != nil {
			t.Err = err
			log.Error().
				Str("file", t.FilePath).
				Int("line", target.lineNum).
				Err(err).
				Msg("unable to extract macro")
			return false
		}
		if len(macros) == 0 {
			continue
		}
		t.macros = append(t.macros, macros...)

		for _, n := range target.nodesToRemove {
			if n.Parent() != nil {
				n.Parent().RemoveChild(n.Parent(), n)
			}
		}

		modified = true
	}

	for _, m := range t.macros {
		var textNodesToReplace []struct {
			node    ast.Node
			val     []byte
			lineNum int
		}

		_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}

			// Leaf text only. Matching whole paragraphs as well caught the
			// same words twice, and a paragraph's text includes whatever is
			// inside its code spans -- so a macro pattern mentioned in
			// `backticks` was expanded along with the prose around it.
			//
			// Both kinds of leaf count. Text is what the document itself was
			// parsed into; a plain String is text an include brought in, which
			// a macro is just as entitled to act on. A String carrying markup
			// is not text at all and is left alone.
			switch n := node.(type) {
			case *ast.Text:
			case *ast.String:
				if n.IsRaw() || n.IsCode() {
					return ast.WalkContinue, nil
				}
			default:
				return ast.WalkContinue, nil
			}

			if insideCode(node) || isExpansion(node) {
				return ast.WalkContinue, nil
			}

			raw := ExtractNodeRawContent(node, reader.Source())
			if len(raw) > 0 && m.Regexp.Match(raw) {
				textNodesToReplace = append(textNodesToReplace, struct {
					node    ast.Node
					val     []byte
					lineNum int
				}{
					node:    node,
					val:     raw,
					lineNum: getNodeLineNumber(node, reader.Source()),
				})
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
				log.Error().
					Str("file", t.FilePath).
					Int("line", item.lineNum).
					Err(err).
					Msg("unable to apply macro")
				return false
			}
			if bytes.Equal(expanded, item.val) {
				continue
			}

			// Whitespace at either end has to be held back and put in again
			// afterwards. Parsing is what loses it: the expansion is parsed as
			// a document of its own, and a document does not begin with a
			// space, so " MYJIRA-123." came back as "<ac:.../>." and the word
			// before it ran into the macro.
			lead, trail, body := splitEdgeSpace(expanded)

			p := newSubParser()
			subDoc := p.Parse(text.NewReader(body))
			convertSegmentsToStrings(subDoc, body)

			parent := item.node.Parent()
			if parent == nil {
				continue
			}

			spliceExpansion(parent, item.node, subDoc, lead, trail)

			modified = true
		}
	}

	return modified
}
