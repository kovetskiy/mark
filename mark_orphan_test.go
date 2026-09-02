package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// orphanFixture publishes two documents under Parent and then removes one of
// them from disk, which is what makes its page an orphan.
func orphanFixture(t *testing.T, onOrphan, under string) (*confluencetest.Server, string, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: onOrphan, OrphanUnder: under,
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(config))

	return server, gone.ID, dir
}

func TestOnOrphanReportLeavesThePage(t *testing.T) {
	server, id, _ := orphanFixture(t, "report", "")

	page := server.Page(id)
	require.NotNil(t, page)
	assert.False(t, page.Trashed, "report must not remove anything")
	assert.False(t, page.Archived)
}

func TestOnOrphanDeleteTrashesThePage(t *testing.T) {
	server, id, _ := orphanFixture(t, "delete", "")

	page := server.Page(id)
	require.NotNil(t, page, "the page must be trashed, not purged")
	assert.True(t, page.Trashed)
}

func TestOnOrphanArchiveArchivesThePage(t *testing.T) {
	server, id, _ := orphanFixture(t, "archive", "")

	page := server.Page(id)
	require.NotNil(t, page)
	assert.True(t, page.Archived)
	assert.False(t, page.Trashed, "archiving is not trashing")
}

// TestOnOrphanRequiresTrackPages: without the manifest there is no way to know
// mark put the page there, which is not a guess to make about deletion.
func TestOnOrphanRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), OnOrphan: "delete", Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--track-pages")
}

func TestOnOrphanRejectsAnUnknownAction(t *testing.T) {
	err := Run(Config{
		BaseURL: "http://127.0.0.1:1", Username: "user", Password: "token",
		Files: "none", OnOrphan: "incinerate", TrackPages: true, Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "report")
}

// TestOnOrphanDryRunActsOnNothing.
func TestOnOrphanDryRunActsOnNothing(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	config.DryRun = true
	require.NoError(t, Run(config))

	assert.False(t, server.Page(gone.ID).Trashed, "a dry run must remove nothing")
}

// TestOnOrphanLeavesAPageWithChildren: trashing a page takes its children with
// it, and those may be pages nobody in this repository ever wrote.
func TestOnOrphanLeavesAPageWithChildren(t *testing.T) {
	server, id := orphanFixtureWithChild(t)

	page := server.Page(id)
	require.NotNil(t, page)
	assert.False(t, page.Trashed,
		"a page holding children must be left alone")
}

func orphanFixtureWithChild(t *testing.T) (*confluencetest.Server, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	// Somebody adds a child page under it in Confluence.
	server.AddPage("DOCS", "Written by hand", "page", gone.ID)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(config))

	return server, gone.ID
}

// TestOnOrphanLeavesEverythingWhenNothingPublished pins the property rather
// than any one mechanism: a run that publishes nothing must remove nothing.
//
// Today an empty file set stops the run before orphans are even considered, so
// this passes with the guard in actOnOrphans removed. It is written against the
// behaviour on purpose -- whichever check is doing the work, moving orphan
// handling ahead of the other one should fail here rather than quietly enable
// mass deletion from a failed checkout.
func TestOnOrphanLeavesEverythingWhenNothingPublished(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "one.md", header+"<!-- Title: One -->\n\nOne.\n")
	writeFile(t, dir, "two.md", header+"<!-- Title: Two -->\n\nTwo.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	one, err := api.FindPage("DOCS", "One", "page")
	require.NoError(t, err)
	require.NotNil(t, one)

	// The checkout is empty this time, but the pattern is the same.
	require.NoError(t, os.Remove(filepath.Join(dir, "one.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "two.md")))

	config.CI = true // so that no files matched is not itself an error
	require.NoError(t, Run(config))

	assert.False(t, server.Page(one.ID).Trashed,
		"a run that published nothing must remove nothing")
}

// TestOrphanUnderLimitsScope: only pages below the named parent are in scope.
func TestOrphanUnderLimitsScope(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Inside", "page", home.ID)
	server.AddPage("DOCS", "Outside", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "in.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: In -->\n\nIn.\n")
	writeFile(t, dir, "out.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Outside -->\n<!-- Title: Out -->\n\nOut.\n")
	writeFile(t, dir, "keep.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: Keep -->\n\nKeep.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", OrphanUnder: "Inside",
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	in, err := api.FindPage("DOCS", "In", "page")
	require.NoError(t, err)
	require.NotNil(t, in)
	out, err := api.FindPage("DOCS", "Out", "page")
	require.NoError(t, err)
	require.NotNil(t, out)

	// Both source files go; only the one under "Inside" is in scope.
	require.NoError(t, os.Remove(filepath.Join(dir, "in.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "out.md")))
	require.NoError(t, Run(config))

	assert.True(t, server.Page(in.ID).Trashed, "the page below Inside is in scope")
	assert.False(t, server.Page(out.ID).Trashed, "the page below Outside is not")
}

// TestOrphanUnderScopesReportingToo is the inconsistency this fixes. The scope
// narrowed what was archived or deleted, while reporting named everything and
// forgetting dropped everything -- so a page deliberately put out of scope was
// announced and then lost track of anyway.
func TestOrphanUnderScopesReportingToo(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Inside", "page", home.ID)
	server.AddPage("DOCS", "Outside", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "in.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: In -->\n\nIn.\n")
	writeFile(t, dir, "out.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Outside -->\n<!-- Title: Out -->\n\nOut.\n")
	writeFile(t, dir, "keep.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: Keep -->\n\nKeep.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "report", OrphanUnder: "Inside",
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	out, err := api.FindPage("DOCS", "Out", "page")
	require.NoError(t, err)
	require.NotNil(t, out)

	// Both documents go. Only the one under "Inside" is mark's business.
	require.NoError(t, os.Remove(filepath.Join(dir, "in.md")))
	require.NoError(t, os.Remove(filepath.Join(dir, "out.md")))
	require.NoError(t, Run(config))

	store := manifest.NewStore(api)

	_, inKnown, err := store.Lookup("DOCS", filepath.Join(dir, "in.md"))
	require.NoError(t, err)
	assert.False(t, inKnown, "an in-scope orphan is reported once and forgotten")

	_, outKnown, err := store.Lookup("DOCS", filepath.Join(dir, "out.md"))
	require.NoError(t, err)
	assert.True(t, outKnown,
		"a page put out of scope must still be remembered, not quietly dropped")
}

// TestOrphanUnderWithoutScopeIsUnchanged: with no scope every tracked page is
// mark's business, which is what it has always been.
func TestOrphanUnderWithoutScopeIsUnchanged(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	_, known, err := manifest.NewStore(api).Lookup("DOCS", filepath.Join(dir, "gone.md"))
	require.NoError(t, err)
	assert.False(t, known, "reported once and then forgotten, as before")
}

// TestOnOrphanCompileOnlyActsOnNothing: --compile-only prints the body it would
// have sent and stops, so it may not delete either. Only --dry-run got the
// read-only manifest, so a compile took the writable one and then returned
// before publishing -- which made every tracked page an orphan.
//
// The guard that keeps a space from being emptied counts what the run saw, and
// a document that has opted out of synchronising is looked up purely to mark it
// seen. One such document is therefore enough to carry the run past the guard
// and on to trashing a page whose source file is still sitting on disk.
func TestOnOrphanCompileOnlyActsOnNothing(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	// keep.md stays exactly where it is and merely stops synchronising.
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n<!-- Synchronized: false -->\n\nKeep.\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

	config.CompileOnly = true
	require.NoError(t, Run(config))

	assert.False(t, server.Page(gone.ID).Trashed, "a compile must remove nothing")
}

// TestOnOrphanLeavesAPageWithChildFolders: the child listing that guards the
// deletion is a listing of pages, and a folder is not one. mark puts folders
// under pages itself, so a page whose children are all folders read as
// childless -- and trashing it takes the folders, and every page inside them,
// along with it.
func TestOnOrphanLeavesAPageWithChildFolders(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeFile(t, dir, "keep.md", header+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", header+"<!-- Title: Gone -->\n\nGone.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	// A folder beneath it, holding a page nobody in this repository wrote.
	folder := server.AddFolder("DOCS", "Manuals", gone.ID, "page")
	server.AddFolder("DOCS", "Nested", folder.ID, "folder")

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	require.NoError(t, Run(config))

	page := server.Page(gone.ID)
	require.NotNil(t, page)
	assert.False(t, page.Trashed, "a page holding folders must be left alone")
}
