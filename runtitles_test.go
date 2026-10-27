package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunTitlesDeclaredBy(t *testing.T) {
	dir := t.TempDir()
	one := writeFile(t, dir, "one.md", parentDoc("Shared"))
	two := writeFile(t, dir, "two.md", parentDoc("shared"))
	other := writeFile(t, dir, "other.md", parentDoc("Other"))
	titles := &runTitles{config: Config{}, files: []string{one, two, other}}

	file, ok := titles.declaredBy("DOCS", "page", "Other")
	assert.True(t, ok)
	assert.Equal(t, other, file)

	_, ok = titles.declaredBy("DOCS", "page", "Shared")
	assert.False(t, ok, "two documents declare it, so neither is followed")

	_, ok = titles.declaredBy("DOCS", "page", "Missing")
	assert.False(t, ok)

	var none *runTitles
	_, ok = none.declaredBy("DOCS", "page", "Other")
	assert.False(t, ok, "a nil index answers for nothing")
}
