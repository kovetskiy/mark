package transformer

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// AttachmentTransformer points links and images at the attachments uploaded
// for them.
//
// Like link resolution this works on the parsed document rather than its text.
// The previous version replaced "](path)" with bytes.ReplaceAll across the
// whole file, so a fenced block showing how to reference an attachment had its
// example rewritten into a download URL -- the same corruption relative links
// suffered, from the same cause.
//
// Matching a destination also removes the need to try long paths before short
// ones. The text version had to sort its replacements by length so that
// "a.jpg" did not eat the front of "a.jpg.jpg"; a destination either is the
// attachment's path or it is not, so there is nothing left to disambiguate.
type AttachmentTransformer struct {
	// Resolve reports the URL for an attachment path, or "" to leave the
	// destination alone.
	Resolve func(target string) string
}

// NewAttachmentTransformer creates an AttachmentTransformer using the given
// resolver.
func NewAttachmentTransformer(resolve func(target string) string) *AttachmentTransformer {
	return &AttachmentTransformer{Resolve: resolve}
}

// Transform implements the parser.ASTTransformer interface.
func (t *AttachmentTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	if t.Resolve == nil {
		return
	}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		// Images as well as links: an attachment is as often shown inline as it
		// is linked to, and the text this replaces matched both.
		var destination *[]byte
		switch n := node.(type) {
		case *ast.Link:
			destination = &n.Destination
		case *ast.Image:
			destination = &n.Destination
		default:
			return ast.WalkContinue, nil
		}

		target := string(*destination)
		if target == "" {
			return ast.WalkContinue, nil
		}

		if resolved := t.Resolve(target); resolved != "" {
			*destination = []byte(resolved)
		}

		return ast.WalkContinue, nil
	})
}
