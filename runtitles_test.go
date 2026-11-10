package mark

import (
	"context"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAParentTitleDeclaredTwiceWarnsEachDocumentOnce: a parent title two of
// the run's documents declare is followed to neither. Each document under it
// is told, as the one whose page is not placed under either, and so is each
// that declares it, once however often the title is looked up.
func TestAParentTitleDeclaredTwiceWarnsEachDocumentOnce(t *testing.T) {
	titles, collectors := ambiguousTitles(t)

	children := map[string]*report.Warnings{
		"child.md": report.NewWarnings("child.md"), "other-child.md": report.NewWarnings("other-child.md"),
	}
	for _, child := range children {
		// Read twice, as a document republished for its deferred links is.
		for range 2 {
			child.Reread()
			titles.warnAmbiguous("DOCS", "Shared", child)
		}
	}

	for file, warnings := range children {
		list := warnings.List()
		require.Len(t, list, 1, file)
		assert.Equal(t, report.WarningPlacement, list[0].Kind)
		assert.Equal(t, file, list[0].File)
		assert.Contains(t, list[0].Message, `parent "Shared" is declared by 2 documents`)
		assert.Contains(t, list[0].Message, "not followed")
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

// TestAnAmbiguousParentFollowedByTheManifestIsNotWarned: a parent title two of
// the run's documents declare is still followed when the manifest recorded the
// chain, so neither the document under it nor the declarers are told it is not.
func TestAnAmbiguousParentFollowedByTheManifestIsNotWarned(t *testing.T) {
	ctx := context.Background()
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	renamed := server.AddPage("DOCS", "Renamed", "page", "")
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	tracker := manifest.NewStore(api)
	tracker.SetRunFiles("*.md", nil)
	require.NoError(t, tracker.RecordParent(ctx, "DOCS", page.ParentPathKey([]string{"Shared"}), renamed.ID))

	titles, declarers := ambiguousTitles(t)
	child := report.NewWarnings("child.md")
	meta := &metadata.Meta{Space: "DOCS", Title: "Child", Parents: []string{"Shared"}}

	require.NoError(t, refreshStaleParents(ctx, tracker, api, meta, titles, child))

	assert.Equal(t, []string{"Renamed"}, meta.Parents, "the recorded chain is followed")
	assert.Empty(t, child.List())
	for file, warnings := range declarers {
		assert.Empty(t, warnings.List(), file)
	}
}

// TestAChainNamingAnAmbiguousParentTwiceIsWarnedOnce: a document whose chain
// names the same ambiguous title twice, in any case, is told once.
func TestAChainNamingAnAmbiguousParentTwiceIsWarnedOnce(t *testing.T) {
	ctx := context.Background()
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	tracker := manifest.NewStore(api)
	tracker.SetRunFiles("*.md", nil)

	titles, _ := ambiguousTitles(t)
	child := report.NewWarnings("child.md")
	meta := &metadata.Meta{Space: "DOCS", Title: "Child", Parents: []string{"Shared", "shared"}}

	require.NoError(t, refreshStaleParents(ctx, tracker, api, meta, titles, child))

	list := child.List()
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Message, "not followed")
}

// ambiguousTitles is a run whose a.md and b.md both declare the page "Shared",
// with the collector each declarer's warnings go to.
func ambiguousTitles(t *testing.T) (*runTitles, map[string]*report.Warnings) {
	t.Helper()

	dir := t.TempDir()
	a := writeFile(t, dir, "a.md", parentDoc("Shared"))
	b := writeFile(t, dir, "b.md", parentDoc("Shared"))

	collectors := map[string]*report.Warnings{
		a: report.NewWarnings(a), b: report.NewWarnings(b),
	}

	return &runTitles{
		files:       []string{a, b},
		warningsFor: func(file string) *report.Warnings { return collectors[file] },
	}, collectors
}
