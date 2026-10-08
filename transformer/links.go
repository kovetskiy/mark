package transformer

import (
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

// LinkTransformer rewrites relative links to the Confluence pages they name.
//
// It works on the parsed document rather than on its text, which is the whole
// point. The previous approach found links with a regular expression and
// replaced them with bytes.ReplaceAll across the file, and both halves of that
// reached places a link cannot be: a fenced block documenting Markdown syntax
// had its example links turned into Confluence URLs, and any code sample
// containing the same text as a real link elsewhere was rewritten along with
// it. goldmark never produces a Link node inside a code span or a fenced
// block, so neither can happen here.
type LinkTransformer struct {
	// Resolve reports what a target should become, or "" to leave it alone.
	Resolve func(target, text string) (string, error)

	// Err holds the first failure, since an AST walk cannot return one.
	Err error
}

// NewLinkTransformer creates a LinkTransformer using the given resolver.
func NewLinkTransformer(resolve func(target, text string) (string, error)) *LinkTransformer {
	return &LinkTransformer{Resolve: resolve}
}

// GetError returns any error encountered while rewriting.
func (t *LinkTransformer) GetError() error {
	return t.Err
}

// Transform implements the parser.ASTTransformer interface.
func (t *LinkTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	if t.Resolve == nil {
		return
	}

	// A failure aborts the walk and comes back out of ast.Walk, which is the
	// only way an error can leave a transformer -- Transform itself cannot
	// return one.
	t.Err = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		link, ok := node.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}

		written := link.Destination.Str(reader.Source())
		if written == "" {
			return ast.WalkContinue, nil
		}

		// The resolver is handed the destination as CommonMark reads it, so
		// that "other\_page.md" and "a&amp;b.md" name the files they spell --
		// the same reading an image destination gets. An ac: link is the
		// exception: the renderer takes its title from the destination as
		// written, and checking a different title from the one published would
		// be checking the wrong page.
		target := written
		if !strings.HasPrefix(written, "ac:") {
			target = LinkDestination(link, reader.Source())
		}

		// The label as written is what the renderer reads for an ac: link.
		resolved, err := t.Resolve(target, LabelText(link, reader.Source()))
		if err != nil {
			return ast.WalkStop, err
		}

		if resolved != "" && resolved != written {
			link.Destination = SourceValue(resolved)
		}

		return ast.WalkContinue, nil
	})
}
