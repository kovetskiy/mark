package mark

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAParentTitleDeclaredTwiceWarnsEachDocumentOnce: a parent title two of
// the run's documents declare is followed to neither; each is told, once
// however often the title is looked up.
func TestAParentTitleDeclaredTwiceWarnsEachDocumentOnce(t *testing.T) {
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Title: Shared -->\n\n"
	writeFile(t, dir, "a.md", header+"A.\n")
	writeFile(t, dir, "b.md", header+"B.\n")

	collectors := map[string]*report.Warnings{}
	titles := &runTitles{
		files: []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")},
		warningsFor: func(file string) *report.Warnings {
			if collectors[file] == nil {
				collectors[file] = report.NewWarnings(file)
			}

			return collectors[file]
		},
	}

	for range 2 {
		_, ok := titles.declaredBy("DOCS", "page", "Shared")
		assert.False(t, ok, "neither is followed")
	}

	require.Len(t, collectors, 2)
	for file, warnings := range collectors {
		list := warnings.List()
		require.Len(t, list, 1, file)
		assert.Equal(t, report.WarningPlacement, list[0].Kind)
		assert.Contains(t, list[0].Message, `parent "Shared" is declared by 2 documents`)
	}
}

// TestRunTitlesDeclaredBy: a title is followed to the one document declaring
// it, compared without regard to case, and to none when two declare it or
// none does; a nil index answers for nothing.
func TestRunTitlesDeclaredBy(t *testing.T) {
	dir := t.TempDir()
	one := writeFile(t, dir, "one.md", parentDoc("Shared"))
	two := writeFile(t, dir, "two.md", parentDoc("shared"))
	other := writeFile(t, dir, "other.md", parentDoc("Other"))
	titles := &runTitles{files: []string{one, two, other}}

	file, ok := titles.declaredBy("DOCS", "page", "OTHER")
	assert.True(t, ok, "titles compare without regard to case")
	assert.Equal(t, other, file)

	_, ok = titles.declaredBy("DOCS", "page", "Shared")
	assert.False(t, ok, "two documents declare it, whatever their case, so neither is followed")

	_, ok = titles.declaredBy("DOCS", "page", "Missing")
	assert.False(t, ok)

	_, ok = titles.declaredBy("OTHER", "page", "Other")
	assert.False(t, ok, "a title is declared in its own space")

	_, ok = titles.declaredBy("DOCS", "blogpost", "Other")
	assert.False(t, ok, "a page is not a blog post")

	var none *runTitles
	_, ok = none.declaredBy("DOCS", "page", "Other")
	assert.False(t, ok, "a nil index answers for nothing")
}
