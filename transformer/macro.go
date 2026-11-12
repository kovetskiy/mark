package transformer

import (
	"bytes"
	"fmt"
	"slices"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/macro"
	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// macroText is the text a macro is applied to: one piece of the document's
// text. Each piece is matched by itself, since a match over several joined
// would be rewritten without the markup between them.
func macroText(node ast.Node, source []byte) []byte {
	switch n := node.(type) {
	case *ast.Text:
		return valueBytes(n.Value, source)
	case *String:
		if !n.Code {
			return slices.Clip(n.Value)
		}
	}

	return nil
}

// keepOuterSpace gives the first and last paragraphs of subDoc the spaces
// that expanded starts and ends with. Parsing drops them, and they are what
// separates a piece of text from the markup on either side of it.
func keepOuterSpace(subDoc ast.Node, expanded []byte) {
	const space = " \t"

	isText := func(n ast.Node) bool {
		return n != nil && (n.Kind() == ast.KindParagraph || n.Kind() == KindTextBlock)
	}

	if lead := expanded[:len(expanded)-len(bytes.TrimLeft(expanded, space))]; len(lead) > 0 && isText(subDoc.FirstChild()) {
		first := subDoc.FirstChild()
		first.InsertBefore(first.FirstChild(), NewString(bytes.Clone(lead)))
	}

	if trail := expanded[len(bytes.TrimRight(expanded, space)):]; len(trail) > 0 && isText(subDoc.LastChild()) {
		subDoc.LastChild().AppendChild(NewString(bytes.Clone(trail)))
	}
}

// MacroTransformer extracts macro directives from HTML comment blocks in the Goldmark AST,
// removes the definition nodes, and expands matching document text nodes into sub-ASTs.
type MacroTransformer struct {
	FilePath    string
	BaseDir     string
	IncludePath string
	// Root is where BaseDir is; the zero Root is the disk.
	Root      attachment.Root
	Templates *template.Template
	Err       error
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
		node       ast.Node
		rawContent []byte
		lineNum    int
	}

	var targets []macroTarget

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		// A directive written as an example is not a directive. The span
		// itself contributes nothing, and its children are skipped too or the
		// walk simply finds the directive one level further down.
		if node.Kind() == ast.KindCodeSpan {
			return ast.WalkSkipChildren, nil
		}

		rawContent := directiveSource(node, reader.Source())
		if rawContent == nil {
			return ast.WalkContinue, nil
		}

		if dir, _ := macro.ParseMacroDirective(rawContent); dir != nil {
			targets = append(targets, macroTarget{
				node:       node,
				rawContent: rawContent,
				lineNum:    getNodeLineNumber(node, reader.Source()),
			})
		}

		return ast.WalkContinue, nil
	})

	modified := false
	var extractedMacros []macro.Macro

	for _, target := range targets {
		macros, _, err := macro.ExtractMacrosIn(t.Root, t.BaseDir, t.IncludePath, target.rawContent, t.Templates)
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
		extractedMacros = append(extractedMacros, macros...)

		if parent := target.node.Parent(); parent != nil {
			parent.RemoveChild(target.node)
		}

		modified = true
	}

	for _, m := range extractedMacros {
		var textNodesToReplace []struct {
			node    ast.Node
			val     []byte
			lineNum int
		}

		_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}

			if node.Kind() == ast.KindCodeSpan {
				return ast.WalkSkipChildren, nil
			}

			raw := macroText(node, reader.Source())
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

			subDoc := parseSubDocument(expanded)
			if err := convertSegmentsToStrings(subDoc, expanded); err != nil {
				t.Err = fmt.Errorf("unable to apply macro %q: %w", m.Regexp.String(), err)
				log.Error().
					Str("file", t.FilePath).
					Int("line", item.lineNum).
					Err(err).
					Msg("unable to apply macro")
				return false
			}

			keepOuterSpace(subDoc, expanded)
			replaceWith(item.node, subDoc)

			modified = true
		}
	}

	return modified
}
