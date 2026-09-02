package page

import (
	"github.com/kovetskiy/mark/v16/parser"
	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// headingAnchor returns the anchor a fragment names in another document.
//
// The fragment is what the author wrote -- the lowercase-and-hyphens slug every
// other Markdown tool produces -- and the anchor is the id mark generates for
// that heading, which keeps its capitals and its punctuation. The same folding
// that matches the two ends of a same-page link matches them here, so a link
// across files behaves exactly as one within a file does.
//
// Returns "" when the fragment names no heading in that document, which leaves
// the link as the author wrote it rather than guessing.
func headingAnchor(document []byte, fragment string) string {
	if fragment == "" {
		return ""
	}

	ids := headingIDs(document)

	// An id written out exactly is already what it names.
	for _, id := range ids {
		if id == fragment {
			return id
		}
	}

	key := transformer.AnchorKey(fragment)
	if key == "" {
		return ""
	}

	var found string

	for _, id := range ids {
		if transformer.AnchorKey(id) != key {
			continue
		}

		// Two headings whose ids differ only in punctuation the folding drops
		// are one key. Choosing either would be a guess, and a link that goes
		// nowhere is better than one that quietly goes to the wrong section.
		if found != "" && found != id {
			return ""
		}

		found = id
	}

	return found
}

// headingIDs lists the ids mark would give the headings of a document, in the
// order they appear.
//
// Parsed with the same id generator the publish uses, since an id that is
// derived differently here would match the wrong heading -- or none.
func headingIDs(document []byte) []string {
	md := goldmark.New(goldmark.WithParserOptions(gparser.WithAutoHeadingID()))

	// The id generator is a context option rather than a parser option, and it
	// is what makes these the ids the publish will use.
	ctx := gparser.NewContext(gparser.WithIDs(parser.NewConfluenceIDs()))

	doc := md.Parser().Parse(text.NewReader(document), gparser.WithContext(ctx))

	var ids []string

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}

		if id, ok := node.AttributeString("id"); ok {
			switch value := id.(type) {
			case []byte:
				ids = append(ids, string(value))
			case string:
				ids = append(ids, value)
			}
		}

		return ast.WalkContinue, nil
	})

	return ids
}
