package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMathDoesNotReachPastItsBlock pins a formula to the block it opens in.
//
// The scan for the closing marker used to run over the rest of the document,
// so two unrelated "$$" in a page -- a price in one paragraph and a mention in
// another -- closed against each other and turned everything between them into
// one equation. The swallowed paragraphs were still blocks of their own, so
// they were published twice: once as the formula's image and once as prose.
func TestMathDoesNotReachPastItsBlock(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "dollars in separate paragraphs",
			source: "A price of $$ in the intro.\n\nSome important paragraph here.\n\nAnother mention of $$ later on.\n",
		},
		{
			name:   "brackets in separate paragraphs",
			source: "An opening \\[ here.\n\nSome important paragraph here.\n\nA closing \\] there.\n",
		},
		{
			name:   "across a heading",
			source: "Costs $$ today.\n\n## A Heading\n\nAnd $$ tomorrow.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, formulas(t, tt.source))
		})
	}
}

// TestMathStillSpansLinesWithinOneBlock is the other half of the bound: a
// display formula is written across several lines often enough that stopping
// at the end of the line would break more than it fixed.
func TestMathStillSpansLinesWithinOneBlock(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "display formula on its own lines",
			source: "$$\n\\int_0^1 x\\,dx\n$$\n",
			want:   []string{"display:\n\\int_0^1 x\\,dx\n"},
		},
		{
			name:   "display formula wrapped mid-paragraph",
			source: "Text before $$a +\nb$$ and text after.\n",
			want:   []string{"display:a +\nb"},
		},
		{
			name:   "bracket form across lines",
			source: "\\[a +\nb\\]\n",
			want:   []string{"display:a +\nb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formulas(t, tt.source))
		})
	}
}
