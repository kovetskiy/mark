package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
)

// TestGetLineCol covers the position arithmetic behind the one error message a
// user is likely to see from a diagram block: "line 12, col 3: mermaid
// rendering failed". A position computed from the wrong offset sends the author
// to the wrong block, which is worse than no position at all.
func TestGetLineCol(t *testing.T) {
	source := []byte("first\nsecond\n\nfourth")

	tests := []struct {
		name     string
		offset   int
		wantLine int
		wantCol  int
	}{
		{"start of the document", 0, 1, 1},
		{"inside the first line", 3, 1, 4},
		{"the newline itself", 5, 1, 6},
		{"start of the second line", 6, 2, 1},
		{"an empty line", 13, 3, 1},
		{"the first character of the last line", 14, 4, 1},
		{"past the end clamps to the end", 1000, 4, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := crenderer.GetLineCol(source, tt.offset)
			assert.Equal(t, tt.wantLine, line, "line")
			assert.Equal(t, tt.wantCol, col, "col")
		})
	}
}
