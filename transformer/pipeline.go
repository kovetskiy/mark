package transformer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// PipelineTransformer runs a slice of AST Transformers iteratively until
// the AST document reaches a fixed point (no further nodes added or modified).
type PipelineTransformer struct {
	Transformers []parser.ASTTransformer
	Err          error
}

// NewPipelineTransformer constructs a PipelineTransformer with the provided AST transformers.
func NewPipelineTransformer(transformers ...parser.ASTTransformer) *PipelineTransformer {
	return &PipelineTransformer{
		Transformers: transformers,
	}
}

// GetError returns any error encountered during pipeline execution.
func (p *PipelineTransformer) GetError() error {
	return p.Err
}

// Transform implements the parser.ASTTransformer interface.
func (p *PipelineTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	for i := 0; i < 10; i++ {
		modified := false
		for _, t := range p.Transformers {
			if mod, ok := t.(interface {
				TransformWithModified(doc *ast.Document, reader text.Reader, pc parser.Context) bool
			}); ok {
				if mod.TransformWithModified(doc, reader, pc) {
					modified = true
				}
			} else {
				t.Transform(doc, reader, pc)
			}

			if errGetter, ok := t.(interface{ GetError() error }); ok {
				if err := errGetter.GetError(); err != nil {
					p.Err = err
					return
				}
			}
		}
		if !modified {
			break
		}
	}
}
