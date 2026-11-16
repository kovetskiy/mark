package mdsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineOf(t *testing.T) {
	original := "<!-- Title: T -->\n\nintro\n!!! note\ntext\n\n!!! note\nmore\n"

	tests := []struct {
		name    string
		derived string
		line    int
		want    int
	}{
		{"unchanged", original, 4, 4},
		{"lines taken out above", "intro\n!!! note\ntext\n\n!!! note\nmore\n", 2, 4},
		{"the second of two lines that read alike", "intro\n!!! note\ntext\n\n!!! note\nmore\n", 5, 7},
		{"lines put in above", "<!-- Title: T -->\n\nintro\nincluded\nlines\n!!! note\ntext\n\n!!! note\nmore\n", 6, 4},
		{"a line that reads alike put in", "intro\n!!! note\nincluded\n!!! note\ntext\n\n!!! note\nmore\n", 4, 0},
		{"a line the original does not hold", "intro\n!!! tip\n", 2, 0},
		{"no such line", "intro\n", 7, 0},
		{"line zero", "intro\n", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, LineOf([]byte(tt.derived), []byte(original), tt.line))
		})
	}
}

// TestLinesOfAnswersEachLine: the function LinesOf returns gives each line
// what LineOf does, however many it is asked for.
func TestLinesOfAnswersEachLine(t *testing.T) {
	original := "<!-- Title: T -->\n\nintro\n!!! note\ntext\n\n!!! note\nmore\n"
	derived := "intro\n!!! note\nincluded\n\n!!! note\ntext\n"

	lineOf := LinesOf([]byte(derived), []byte(original))

	for line := 0; line <= 8; line++ {
		assert.Equal(t, LineOf([]byte(derived), []byte(original), line), lineOf(line), "line %d", line)
	}

	assert.Equal(t, []int{3, 4, 0, 0, 7, 5}, []int{lineOf(1), lineOf(2), lineOf(3), lineOf(4), lineOf(5), lineOf(6)})
}
