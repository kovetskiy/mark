package parser_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/ast"
)

// TestConfluenceIDsKeepsNonASCII pins the id of a heading written in a script
// that has no ASCII in it.
//
// Every multi-byte rune used to be dropped, so such a heading had nothing left
// and took the "heading" fallback meant for a title made entirely of
// punctuation. A page with two of them got "heading" and "heading-1", which no
// author could link to and which say nothing about what they name.
func TestConfluenceIDsKeepsNonASCII(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"概要", "概要"},
		{"Обзор раздела", "Обзор-раздела"},
		{"Über uns", "Über-uns"},
		{"混合 Heading 2", "混合-Heading-2"},
		// Punctuation alone still has nothing to build an id from.
		{"!!!", "heading"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			ids := cparser.NewConfluenceIDs()
			assert.Equal(t, tt.want, string(ids.Generate([]byte(tt.value), ast.KindHeading)))
		})
	}
}

// TestConfluenceIDsSeparatesNonASCIIHeadings covers the consequence of the
// above for a page rather than a heading: two headings that differ must get
// two ids, not one id and a numbered duplicate of it.
func TestConfluenceIDsSeparatesNonASCIIHeadings(t *testing.T) {
	ids := cparser.NewConfluenceIDs()

	assert.Equal(t, "概要", string(ids.Generate([]byte("概要"), ast.KindHeading)))
	assert.Equal(t, "詳細", string(ids.Generate([]byte("詳細"), ast.KindHeading)))
}

// TestConfluenceIDsBuildsTheIDFromALinkLabel pins the id of a heading that
// contains a link.
//
// goldmark generates the id from the raw heading line, before anything has
// parsed the inlines in it, so the URL used to land in the id:
// "Heading-with-linkhttps//x.com-and-code". Nothing an author could write in
// "#..." would name that, so a link to such a heading never resolved.
func TestConfluenceIDsBuildsTheIDFromALinkLabel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "inline link",
			value: "Heading with [link](https://x.com) and code",
			want:  "Heading-with-link-and-code",
		},
		{
			name:  "link with a title",
			value: `See [the guide](https://x.com/a "Guide")`,
			want:  "See-the-guide",
		},
		{
			name:  "image",
			value: "Heading with ![diagram](img/a.png)",
			want:  "Heading-with-diagram",
		},
		{
			name:  "two links",
			value: "[one](https://a.com) and [two](https://b.com)",
			want:  "one-and-two",
		},
		// Brackets that are not a link are ordinary text, and the characters
		// they hold have always survived into the id.
		{
			name:  "brackets that open nothing",
			value: "Heading [not a link] here",
			want:  "Heading-not-a-link-here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := cparser.NewConfluenceIDs()
			assert.Equal(t, tt.want, string(ids.Generate([]byte(tt.value), ast.KindHeading)))
		})
	}
}

// TestConfluenceIDsSurvivesInvalidUTF8 covers the one way a rune-at-a-time
// loop can fail to terminate. A heading read from a file is arbitrary bytes,
// and a decoder that returned a zero width on a bad one would spin forever.
func TestConfluenceIDsSurvivesInvalidUTF8(t *testing.T) {
	ids := cparser.NewConfluenceIDs()

	assert.Equal(t, "ab", string(ids.Generate([]byte{'a', 0xff, 0xfe, 'b'}, ast.KindHeading)))
}
