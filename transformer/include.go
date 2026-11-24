package transformer

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/includes"
	"github.com/kovetskiy/mark/v17/internal/logctx"
	"github.com/kovetskiy/mark/v17/report"
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
	// Context is the context the transformer expands in, whose zerolog
	// logger it logs to; nil is context.Background.
	Context context.Context
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

// context is t.Context, or context.Background for a transformer given none.
func (t *IncludeTransformer) context() context.Context {
	if t.Context == nil {
		return context.Background()
	}

	return t.Context
}

// Transform implements the parser.ASTTransformer interface.
func (t *IncludeTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	t.TransformWithModified(doc, reader, pc)
}

// TransformWithModified transforms the AST and returns true if any modifications were made.
func (t *IncludeTransformer) TransformWithModified(doc *ast.Document, reader text.Reader, pc parser.Context) bool {
	targets := directiveNodes(doc, reader.Source(), func(raw []byte) bool {
		dir, _ := includes.ParseIncludeDirective(raw)

		return dir != nil
	})

	if len(targets) == 0 {
		return false
	}

	modified := false
	for _, target := range targets {
		tmpl, expanded, _, err := includes.ProcessIncludesInContext(t.context(), t.Root, t.BaseDir, t.IncludePath, target.raw, t.Templates, nil, t.Warnings)
		if err != nil {
			t.Err = err
			logctx.From(t.context()).Error().
				Str("file", t.FilePath).
				Int("line", target.line).
				Err(err).
				Msg("unable to process include")
			return false
		}
		if bytes.Equal(expanded, target.raw) {
			continue
		}
		t.Templates = tmpl

		if target.node.Parent() == nil {
			continue
		}

		subDoc := parseInPlaceOf(target.node, expanded)
		if err := convertSegmentsToStrings(subDoc, expanded); err != nil {
			t.Err = fmt.Errorf("unable to process include at line %d: %w", target.line, err)
			logctx.From(t.context()).Error().
				Str("file", t.FilePath).
				Int("line", target.line).
				Err(err).
				Msg("unable to process include")
			return false
		}

		replaceWith(target.node, subDoc)

		modified = true
	}

	return modified
}
