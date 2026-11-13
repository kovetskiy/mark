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

// joinText takes into node the text nodes that follow it with nothing
// between them. goldmark ends a Text at a character that might have opened
// markup -- a "[", a "*" -- and a pattern that holds one is matched against
// the text as it was written, not against those parts.
func joinText(node ast.Node, source []byte) {
	for next := node.NextSibling(); next != nil && joinNext(node, next, source); next = node.NextSibling() {
		node.Parent().RemoveChild(next)
	}
}

// joinNext appends next's text to node's and reports whether it did: two
// Texts that are one run of the source with no line break between them, or
// two Strings of text.
func joinNext(node, next ast.Node, source []byte) bool {
	if len(node.Attributes()) > 0 || len(next.Attributes()) > 0 {
		return false
	}

	switch a := node.(type) {
	case *ast.Text:
		b, ok := next.(*ast.Text)
		if !ok || a.SoftLineBreak() || a.HardLineBreak() ||
			a.Value.IsEmpty() || b.Value.IsEmpty() || a.Value.IsOwned() || b.Value.IsOwned() ||
			a.Value.Index().Stop != b.Value.Index().Start || valueBytes(b.Value, source) == nil {
			return false
		}

		a.Value = a.Value.WithStop(b.Value.Index().Stop)
		a.SetSoftLineBreak(b.SoftLineBreak())
		a.SetHardLineBreak(b.HardLineBreak())

		return true
	case *String:
		b, ok := next.(*String)
		if !ok || a.Code || b.Code {
			return false
		}

		a.Value = append(slices.Clip(a.Value), b.Value...)

		return true
	}

	return false
}

// lineGuard stands in for what precedes a piece of text on its line while
// the piece's expansion is parsed: by itself "- done", after a link, would
// open a list. It reads as white space, as the start of a line does, and
// unlike a space it is not dropped from the start of a paragraph.
const lineGuard = "\u00a0"

// outerSpace is the white space parsing drops from the ends of a line.
const outerSpace = " \t"

// startsBlock reports whether node is the first thing in a paragraph, where
// text can be the start of a block of another kind. In a heading or a table
// cell it follows the block's own marker, and inside markup the markup's.
func startsBlock(node ast.Node) bool {
	return node.PreviousSibling() == nil && isParagraph(node.Parent())
}

// parseExpansion parses what a macro made of the piece of text at node into
// a sub-document that can be put in its place. Unless the piece starts a
// block, its first line is read as the rest of a line and opens none.
//
// The spaces the expansion starts and ends with are put back: parsing drops
// them, and they separate the piece from the markup on either side of it.
func parseExpansion(node ast.Node, expanded []byte) (ast.Node, error) {
	midLine := !startsBlock(node)

	source := bytes.TrimRight(expanded, outerSpace)
	trail := expanded[len(source):]

	var lead []byte
	if midLine {
		body := bytes.TrimLeft(source, outerSpace)
		lead = source[:len(source)-len(body)]
		source = append([]byte(lineGuard), body...)
	}

	subDoc := parseSubDocument(source)
	if err := convertSegmentsToStrings(subDoc, source); err != nil {
		return nil, err
	}

	if midLine {
		if guard, ok := subDoc.FirstChild().FirstChild().(*String); ok {
			guard.Value = append(bytes.Clone(lead), bytes.TrimPrefix(guard.Value, []byte(lineGuard))...)
		}
	}

	if last := subDoc.LastChild(); len(trail) > 0 && last != nil && isParagraph(last) {
		last.AppendChild(NewString(bytes.Clone(trail)))
	}

	return subDoc, nil
}

// MacroTransformer extracts macro directives from HTML comment blocks in the Goldmark AST,
// removes the definition nodes, and expands matching document text nodes into sub-ASTs.
//
// A pattern is matched against one run of text at a time: it does not match
// across inline markup, raw HTML or a line break, and not in a code span or
// an image's description.
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
	targets := directiveNodes(doc, reader.Source(), func(raw []byte) bool {
		dir, _ := macro.ParseMacroDirective(raw)

		return dir != nil
	})

	modified := false
	var extractedMacros []macro.Macro

	for _, target := range targets {
		macros, _, err := macro.ExtractMacrosIn(t.Root, t.BaseDir, t.IncludePath, target.raw, t.Templates)
		if err != nil {
			t.Err = err
			log.Error().
				Str("file", t.FilePath).
				Int("line", target.line).
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

			// An image's text is its description, which holds no markup.
			if node.Kind() == ast.KindImage {
				return ast.WalkSkipChildren, nil
			}

			joinText(node, reader.Source())

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

			subDoc, err := parseExpansion(item.node, expanded)
			if err != nil {
				t.Err = fmt.Errorf("unable to apply macro %q: %w", m.Regexp.String(), err)
				log.Error().
					Str("file", t.FilePath).
					Int("line", item.lineNum).
					Err(err).
					Msg("unable to apply macro")
				return false
			}

			replaceWith(item.node, subDoc)

			modified = true
		}
	}

	return modified
}
