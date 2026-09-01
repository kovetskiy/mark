package transformer

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// XMLWellFormedTransformer rewrites the raw HTML an author wrote so that it is
// well-formed XML, which is what Confluence storage format is.
//
// Two shapes of hand-written HTML are legal in Markdown and rejected by
// Confluence, and it rejects the whole page rather than the element:
//
//   - a void element left open, `<br>` rather than `<br />`. Writing `<br>` in
//     a table cell is the standard way to get more than one line into one, so
//     this took out pages that were otherwise plain Markdown.
//   - an HTML comment containing `--`, which XML does not allow anywhere in a
//     comment body.
//
// Only the fragments that need it are rewritten; a node whose bytes are already
// well-formed is left exactly as the author wrote it.
type XMLWellFormedTransformer struct{}

// NewXMLWellFormedTransformer creates a new instance of XMLWellFormedTransformer.
func NewXMLWellFormedTransformer() *XMLWellFormedTransformer {
	return &XMLWellFormedTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *XMLWellFormedTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	// Collect first, mutate after: replacing a node clears its NextSibling and
	// ast.Walk is iterating over that pointer, so mutating mid-walk abandons
	// every remaining sibling under the same parent.
	var nodes []ast.Node

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node.(type) {
		case *ast.HTMLBlock, *ast.RawHTML:
			nodes = append(nodes, node)
		}

		return ast.WalkContinue, nil
	})

	source := reader.Source()

	for _, node := range nodes {
		parent := node.Parent()
		if parent == nil {
			continue
		}

		fixed, changed := wellFormedHTML(ExtractNodeRawContent(node, source))
		if !changed {
			continue
		}

		// SetCode, because the replacement is storage format already: a plain
		// or "raw" string goes through Writer.RawWrite, which would escape the
		// very markup being repaired.
		replacement := ast.NewString(fixed)
		replacement.SetCode(true)

		parent.InsertBefore(parent, node, replacement)
		parent.RemoveChild(parent, node)
	}
}

// wellFormedHTML returns raw with void elements closed and comment bodies made
// legal, and reports whether anything had to change. Every other byte, tag and
// attribute is passed through untouched -- the input is the author's markup,
// not something to normalise.
func wellFormedHTML(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	var buf bytes.Buffer
	changed := false

	tokenizer := html.NewTokenizer(bytes.NewReader(raw))

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}

		// Raw is only valid until the next call to Next, and every token's Raw
		// concatenated is the input, so writing it is a faithful passthrough.
		token := tokenizer.Raw()

		switch tokenType {
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			if isVoidElement(string(name)) && bytes.HasSuffix(token, []byte(">")) {
				buf.Write(token[:len(token)-1])
				buf.WriteString(" />")
				changed = true
				continue
			}

		case html.CommentToken:
			// A bogus comment is how the tokenizer reports <![CDATA[...]]>,
			// which stdlib templates emit on purpose and which is not a
			// comment at all.
			if bytes.HasPrefix(token, []byte("<!--")) && bytes.HasSuffix(token, []byte("-->")) && len(token) >= 7 {
				body, bodyChanged := wellFormedCommentBody(token[4 : len(token)-3])
				if bodyChanged {
					buf.WriteString("<!--")
					buf.Write(body)
					buf.WriteString("-->")
					changed = true
					continue
				}
			}
		}

		buf.Write(token)
	}

	if !changed {
		return raw, false
	}

	return buf.Bytes(), true
}

// wellFormedCommentBody removes the runs of hyphens that XML forbids inside a
// comment: `--` anywhere in the body, and a trailing `-` that would run into
// the closing `-->`. A comment is invisible on the page, so collapsing the
// hyphens costs the author nothing and keeps the page publishable.
func wellFormedCommentBody(body []byte) ([]byte, bool) {
	var buf bytes.Buffer

	hyphens := 0
	for _, c := range body {
		if c == '-' {
			hyphens++
			if hyphens > 1 {
				continue
			}
		} else {
			hyphens = 0
		}
		buf.WriteByte(c)
	}

	clean := bytes.TrimRight(buf.Bytes(), "-")
	if bytes.Equal(clean, body) {
		return body, false
	}

	return clean, true
}
