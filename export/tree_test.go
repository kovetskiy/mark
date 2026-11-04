package export

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// A page id goes into the paths a tree is written to, so one that could
// leave the output directory is refused.
func TestCheckPageID(t *testing.T) {
	assert.NoError(t, checkPageID("123456"))
	for _, id := range []string{"", "..", "../..", "12/34", `12\34`, "12a", " 12"} {
		assert.Error(t, checkPageID(id), "%q", id)
	}
}

// A slug is cut to slugMax bytes on a character's boundary, and a short
// title's slug is left whole.
func TestSlugIsCapped(t *testing.T) {
	assert.Equal(t, "c", slug("C++"))
	assert.Equal(t, "deep-dive", slug("Deep dive"))
	assert.Equal(t, strings.Repeat("a", slugMax), slug(strings.Repeat("A", 254)))

	for _, title := range []string{
		strings.Repeat("a", 254),
		strings.Repeat("ab ", 85),
		strings.Repeat("文", 85),
		"x" + strings.Repeat("文", 84),
		strings.Repeat("é", 200),
	} {
		s := slug(title)
		assert.LessOrEqual(t, len(s), slugMax, "%q", title)
		assert.Greater(t, len(s), slugMax-utf8.UTFMax, "%q", title)
		assert.True(t, utf8.ValidString(s), "%q", s)
		assert.False(t, strings.HasSuffix(s, "-"), "%q", s)
	}
}
