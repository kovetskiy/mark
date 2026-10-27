package export

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/internal/goldmarktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// A title ending in # keeps it in its directory's index heading, which would
// otherwise read the #s as a closing sequence.
func TestOKFIndexHeadingKeepsTrailingHashes(t *testing.T) {
	for title, heading := range map[string]string{
		"C #":      `# C \#`,
		"Notes ##": `# Notes #\#`,
		"#":        `# \#`,
		"C#":       `# C\#`,
	} {
		t.Run(title, func(t *testing.T) {
			idx := okfIndexes("out", "x", []planned{
				{id: "1", title: title, output: "out/c.md"},
				{id: "2", title: "Child", output: "out/c/child.md"},
			})
			index := idx[filepath.Join("out", "c", "index.md")]
			assert.Contains(t, index, heading+"\n")

			var html bytes.Buffer
			require.NoError(t, goldmarktest.New().Convert([]byte(index), &html))
			assert.Contains(t, html.String(), "<h1>"+title+"</h1>")
		})
	}
}
