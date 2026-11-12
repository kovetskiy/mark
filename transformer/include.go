package transformer

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/includes"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// IncludeTransformer transforms <!-- Include: ... --> directives in the Goldmark AST
// by reading, expanding, and inserting the parsed AST nodes of included templates.
type IncludeTransformer struct {
	FilePath    string
	BaseDir     string
	IncludePath string
	// Root is where BaseDir is; the zero Root is the disk.
	Root      attachment.Root
	Templates *template.Template
	Err       error
	// Warnings collects what is worth saying about the includes expanded;
	// nil only logs it.
	Warnings *report.Warnings
}

// NewIncludeTransformer creates a new IncludeTransformer instance.
func NewIncludeTransformer(filePath string, baseDir string, includePath string, tmpl *template.Template) *IncludeTransformer {
	if tmpl == nil {
		tmpl = template.New("stdlib")
	}
	return &IncludeTransformer{
		FilePath:    filePath,
		BaseDir:     baseDir,
		IncludePath: includePath,
		Templates:   tmpl,
	}
}

// GetError returns any error encountered during AST transformation.
func (t *IncludeTransformer) GetError() error {
	return t.Err
}

// Transform implements the parser.ASTTransformer interface.
func (t *IncludeTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	t.TransformWithModified(doc, reader, pc)
}

// TransformWithModified transforms the AST and returns true if any modifications were made.
func (t *IncludeTransformer) TransformWithModified(doc *ast.Document, reader text.Reader, pc parser.Context) bool {
	type includeTarget struct {
		node       ast.Node
		rawContent []byte
		lineNum    int
	}

	var targets []includeTarget

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

		if dir, _ := includes.ParseIncludeDirective(rawContent); dir != nil {
			targets = append(targets, includeTarget{
				node:       node,
				rawContent: rawContent,
				lineNum:    getNodeLineNumber(node, reader.Source()),
			})
		}

		return ast.WalkContinue, nil
	})

	if len(targets) == 0 {
		return false
	}

	modified := false
	for _, target := range targets {
		tmpl, expanded, _, err := includes.ProcessIncludesIn(t.Root, t.BaseDir, t.IncludePath, target.rawContent, t.Templates, nil, t.Warnings)
		if err != nil {
			t.Err = err
			log.Error().
				Str("file", t.FilePath).
				Int("line", target.lineNum).
				Err(err).
				Msg("unable to process include")
			return false
		}
		if bytes.Equal(expanded, target.rawContent) {
			continue
		}
		t.Templates = tmpl

		subDoc := parseSubDocument(expanded)
		if err := convertSegmentsToStrings(subDoc, expanded); err != nil {
			t.Err = fmt.Errorf("unable to process include at line %d: %w", target.lineNum, err)
			log.Error().
				Str("file", t.FilePath).
				Int("line", target.lineNum).
				Err(err).
				Msg("unable to process include")
			return false
		}

		if target.node.Parent() == nil {
			continue
		}

		replaceWith(target.node, subDoc)

		modified = true
	}

	return modified
}
