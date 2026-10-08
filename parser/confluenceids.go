package parser

import (
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/util"
)

// reInlineLink matches a link or an image as it is written in the source.
//
// goldmark generates a heading id from the raw heading line, before any inline
// parsing has happened, so "## Heading with [link](https://x.com)" arrived here
// with the URL still in it and produced "Heading-with-linkhttps//x.com" -- an
// id no slug an author could write would ever name. The label is the part the
// reader sees, so it is the part the id is built from.
var reInlineLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// ConfluenceIDGenerator is the parser.IDGenerator goldmark is given for
// Confluence-compatible header anchor IDs, preserving '/', '_', '.', and '-'
// characters in heading IDs.
//
// It returns the base id only. goldmark's parser.IDs keeps the ids a document
// has used, the ones an author wrote as {#id} included, and numbers a repeat
// "-1", "-2" and so on, which is what ConfluenceIDs does for itself.
type ConfluenceIDGenerator struct{}

// Generate implements parser.IDGenerator.
func (ConfluenceIDGenerator) Generate(value []byte, kind ast.NodeKind) []byte {
	return confluenceID(value, kind)
}

// ConfluenceIDs generates Confluence-compatible header anchor IDs and keeps
// them unique, for a caller that has no parser.IDs to do the second part.
type ConfluenceIDs struct {
	Values map[string]bool
}

// NewConfluenceIDs creates a new ConfluenceIDs instance.
func NewConfluenceIDs() *ConfluenceIDs {
	return &ConfluenceIDs{
		Values: make(map[string]bool),
	}
}

// Generate returns the id for value, numbered if it has been used before.
func (s *ConfluenceIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	if s.Values == nil {
		s.Values = make(map[string]bool)
	}
	result := confluenceID(value, kind)
	if _, ok := s.Values[util.BytesToReadOnlyString(result)]; !ok {
		s.Values[util.BytesToReadOnlyString(result)] = true
		return result
	}
	for i := 1; ; i++ {
		newResult := fmt.Sprintf("%s-%d", result, i)
		if _, ok := s.Values[newResult]; !ok {
			s.Values[newResult] = true
			return []byte(newResult)
		}
	}
}

// confluenceID is the base id for value: the characters Confluence keeps in an
// anchor, before any numbering.
func confluenceID(value []byte, kind ast.NodeKind) []byte {
	value = reInlineLink.ReplaceAll(value, []byte("$1"))
	value = util.TrimLeftSpace(value)
	value = util.TrimRightSpace(value)
	result := []byte{}
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRune(value[i:])
		i += size

		// Every multi-byte rune used to be dropped, which left a heading
		// written in a non-Latin script with nothing at all: "## 概要" fell
		// through to the literal id "heading", and a second such heading in
		// the same page became "heading-1". Confluence holds non-ASCII ids
		// perfectly well -- what matters is only that a character is an id
		// character, which unicode.IsLetter and unicode.IsDigit answer for
		// every script rather than for one.
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' || r == '_' || r == '.':
			result = utf8.AppendRune(result, r)
		case unicode.IsSpace(r) || r == '-':
			result = append(result, '-')
		}
	}
	if len(result) == 0 {
		if kind == ast.KindHeading {
			return []byte("heading")
		}
		return []byte("id")
	}
	return result
}

func (s *ConfluenceIDs) Put(value []byte) {
	if s.Values == nil {
		s.Values = make(map[string]bool)
	}
	s.Values[util.BytesToReadOnlyString(value)] = true
}
