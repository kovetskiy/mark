package mark

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

// getsFor counts the GETs the fake received for exactly one content path,
// which is what a scope check costs: one GetPageByIDExpanded per orphan.
func getsFor(server *confluencetest.Server, pageID string) int {
	var n int
	for _, request := range server.Requests() {
		if request.Method == http.MethodGet && request.Path == "/rest/api/content/"+pageID {
			n++
		}
	}

	return n
}

// TestOrphanScopeIsCheckedOnce pins the class of bug where the same remote
// question is asked twice. handleOrphans filtered its candidates with InScope
// and HandleOrphans then asked again about every survivor, so the one operation
// in mark that trashes pages cost twice the API calls it needed -- for an
// answer that had just been computed.
func TestOrphanScopeIsCheckedOnce(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Inside", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "gone.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: Gone -->\n\nGone.\n")
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
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	server.ResetRequests()
	require.NoError(t, Run(config))

	assert.True(t, server.Page(gone.ID).Trashed, "the orphan is still acted on")
	assert.Equal(t, 1, getsFor(server, gone.ID),
		"an orphan's ancestry should be read once, not once per caller that wants the scope")
}

// writesTo lists the requests that could have changed something on the server:
// anything that is not a read. A dry run must send none of them.
func writesTo(server *confluencetest.Server) []string {
	var writes []string
	for _, r := range server.Requests() {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writes = append(writes, r.Method+" "+r.Path)
		}
	}
	return writes
}

// publishTracked publishes every *.md in dir with tracking on and
// --on-orphan delete, returning the config so a dry run can repeat it.
func publishTracked(t *testing.T, server *confluencetest.Server, dir string) Config {
	t.Helper()
	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.OnOrphan = "delete"
	require.NoError(t, Run(config))
	return config
}

// TestDryRunDoesNotReportPublishedPagesAsOrphans: a dry run over documents that
// are all still present, after a tracked publish, must not read them as gone.
// The dry run found each page by title and returned before recording it, so
// every tracked path looked unseen: it reported the whole set as orphans, and
// with --on-orphan delete only the "nothing was published" guard in mark.go
// stopped it from listing each one as to be deleted.
func TestDryRunDoesNotReportPublishedPagesAsOrphans(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	a := writeFile(t, dir, "a.md", markdownWithTitle("A"))
	b := writeFile(t, dir, "b.md", markdownWithTitle("B"))
	config := publishTracked(t, server, dir)

	pageA, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, pageA)
	pageB, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)
	require.NotNil(t, pageB)

	server.ResetRequests()

	dry := config
	dry.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(dry)) })

	// The capture works, at the level the orphan report is written at: without
	// this, a quieter logger would make the assertions below pass vacuously.
	assert.Contains(t, logged, "processing "+inLog(a))
	assert.Contains(t, logged, "processing "+inLog(b))

	// What the dry run used to log: every page reported as an orphan, then the
	// "nothing was published" guard holding the deletion back.
	assert.NotContains(t, logged, "had no matching source file",
		"every document is still there")
	assert.NotContains(t, logged, "nothing was published in this run")
	// The guard above stopped the original bug short of this line; this
	// assertion guards against a regression that gets past it. The text comes
	// from the "page %q would be %sd" format in page/orphan.go.
	assert.NotContains(t, logged, "would be deleted")

	assert.Empty(t, writesTo(server), "a dry run must not change anything")
	assert.False(t, server.Page(pageA.ID).Trashed)
	assert.False(t, server.Page(pageB.ID).Trashed)
}

// TestDryRunStillReportsRealOrphans is the other half: recording the pages a dry
// run finds must not hide the one that is really gone. It is reported, and
// would be deleted, but the dry run leaves it where it is.
func TestDryRunStillReportsRealOrphans(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	a := writeFile(t, dir, "a.md", markdownWithTitle("A"))
	b := writeFile(t, dir, "b.md", markdownWithTitle("B"))
	config := publishTracked(t, server, dir)

	pageA, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, pageA)
	pageB, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)
	require.NotNil(t, pageB)

	require.NoError(t, os.Remove(b))
	server.ResetRequests()

	dry := config
	dry.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(dry)) })

	assert.Contains(t, logged, "processing "+inLog(a))
	assert.Contains(t, logged, "1 tracked page(s) had no matching source file in this run: "+filepath.ToSlash(b),
		"b.md is gone, and only b.md")
	// "page %q would be %sd" in page/orphan.go; the quotes are escaped because
	// zerolog writes the message as JSON.
	assert.Contains(t, logged, `page \"B\" would be deleted`)
	assert.NotContains(t, logged, `page \"A\" would be deleted`)

	assert.Empty(t, writesTo(server), "a dry run must not change anything")
	assert.False(t, server.Page(pageA.ID).Trashed)
	assert.False(t, server.Page(pageB.ID).Trashed, "a dry run only says what it would delete")
}

// inLog is s as it reads in the captured log, which zerolog writes as JSON: a
// Windows path has its backslashes doubled.
func inLog(s string) string {
	b, _ := json.Marshal(s)

	return string(b[1 : len(b)-1])
}

// TestTrackingReportsOrphansWithoutDeleting: a file that disappears from the
// glob is reported, and the page it published to is left completely alone.
// Phase 1 never deletes.
func TestTrackingReportsOrphansWithoutDeleting(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
	writeFile(t, dir, "drop.md", markdownWithTitle("Drop"))
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

	dropped, err := api.FindPage("DOCS", "Drop", "page")
	require.NoError(t, err)
	require.NotNil(t, dropped)

	require.NoError(t, os.Remove(filepath.Join(dir, "drop.md")))
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

	assert.NotNil(t, server.Page(dropped.ID),
		"the page of a deleted file must survive: phase 1 reports, it does not prune")
}

// TestTrackingSuppressesOrphanReportOnFailedRuns pins the branch that used to be
// unreachable. A file that failed to process is indistinguishable from one that
// was deleted, so reporting it as missing would be actively misleading.
func TestTrackingSuppressesOrphanReportOnFailedRuns(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("A"))
	writeFile(t, dir, "b.md", markdownWithTitle("B"))
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

	// b.md now fails to process rather than disappearing.
	writeFile(t, dir, "b.md", "# Broken\n\nNo metadata.\n")

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.ContinueOnError = true
	require.Error(t, Run(config))

	assert.NotContains(t, logged.String(), "had no matching source file",
		"a run with errors must not report unpublished pages as missing")
}

// TestOrphansAreScopedToTheGlob: a run narrowed with --files says nothing about
// files outside it. Reporting them as missing buries the handful of genuine
// deletions in a list of files that are perfectly present.
func TestOrphansAreScopedToTheGlob(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "guides"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notes"), 0o750))
	writeFile(t, dir, "guides/a.md", markdownWithTitle("A"))
	writeFile(t, dir, "notes/b.md", markdownWithTitle("B"))

	// Publish each subtree with its own glob.
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "guides", "*.md"))))
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "notes", "*.md"))))

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	// Re-run only the guides. notes/b.md is untouched and still present.
	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "guides", "*.md"))))

	assert.NotContains(t, logged.String(), "had no matching source file",
		"a run scoped to one directory must not report another's files as missing")
}

// TestDeletedFileIsReportedOnceThenForgotten: left in place, a deletion is
// reported on every run forever, which teaches people to ignore the one message
// worth reading.
func TestDeletedFileIsReportedOnceThenForgotten(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
	writeFile(t, dir, "drop.md", markdownWithTitle("Drop"))
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(trackingConfig(server, glob)))

	require.NoError(t, os.Remove(filepath.Join(dir, "drop.md")))

	first := captureLog(t, func() { require.NoError(t, Run(trackingConfig(server, glob))) })
	assert.Contains(t, first, "had no matching source file",
		"the deletion should be reported the first time it is noticed")

	second := captureLog(t, func() { require.NoError(t, Run(trackingConfig(server, glob))) })
	assert.NotContains(t, second, "had no matching source file",
		"and not on every run thereafter")
}
