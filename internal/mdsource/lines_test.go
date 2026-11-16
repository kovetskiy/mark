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
