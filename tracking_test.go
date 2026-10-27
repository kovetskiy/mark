package mark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackedPageIsRetitledNotDuplicated is the behaviour the manifest exists
// for. mark finds a page by title, so changing the title makes the existing page
// unfindable and a second one is published beside it. With tracking on, the file
// path resolves to the page that was published last time and it is renamed in
// place.
func TestTrackedPageIsRetitledNotDuplicated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Original Title"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	before, err := api.FindPage(context.Background(), "DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	// The Title header changes; the file does not move.
	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	after := server.Page(before.ID)
	require.NotNil(t, after)
	assert.Equal(t, "Renamed Title", after.Title,
		"the original page should have been retitled in place")

	assert.Equal(t, 0, countPagesTitled(t, server, "Original Title"),
		"nothing should still be published under the old title")
}

// TestUntrackedTitleChangeStillDuplicates is the control: without the flag the
// old behaviour is unchanged, which is what makes the test above meaningful.
func TestUntrackedTitleChangeStillDuplicates(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, "")
	config.TrackPages = false

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Original Title"))
	config.Files = file
	require.NoError(t, Run(context.Background(), config))

	before, err := api.FindPage(context.Background(), "DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "Original Title", server.Page(before.ID).Title,
		"without tracking the first page is left behind under its old title")
	assert.Equal(t, 1, countPagesTitled(t, server, "Renamed Title"),
		"and a second page now exists under the new one")
}

// TestTrackedRetitleHappensEvenWhenContentIsUnchanged covers the interaction
// with --changes-only: the content fingerprint matches, so the update would be
// skipped, and the page would keep its old title forever.
func TestTrackedRetitleHappensEvenWhenContentIsUnchanged(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	body := `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: %s -->

Body that does not change.
`
	file := writeFile(t, dir, "doc.md", fmt.Sprintf(body, "First"))
	config := trackingConfig(server, file)
	config.ChangesOnly = true
	require.NoError(t, Run(context.Background(), config))

	before, err := api.FindPage(context.Background(), "DOCS", "First", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	// Only the title changes; the rendered body is byte-identical.
	writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Second"))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "Second", server.Page(before.ID).Title,
		"a retitle must not be skipped just because the content hash matched")
}

// TestTrackedPageDeletedInConfluenceIsRecreated: someone removed the page from
// Confluence by hand. The manifest still names it, and pointing at a page that
// is not there must not wedge the run.
func TestTrackedPageDeletedInConfluenceIsRecreated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	published, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	server.DeletePage(published.ID)

	// Retitle too, so the title lookup also misses and resolution is forced
	// through the manifest entry that now dangles.
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc Renamed"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)),
		"a dangling manifest entry must not fail the run")

	assert.Equal(t, 1, countPagesTitled(t, server, "Doc Renamed"),
		"the page should have been created afresh")
}

// TestTrackingSavesManifestEvenWhenAFileFails covers the case --continue-on-error
// exists for. The manifest used to be written only after an early return on
// hasErrors, so one unpublishable file discarded the mapping for every page that
// had published perfectly well, and the next run had no protection at all.
func TestTrackingSavesManifestEvenWhenAFileFails(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	good := writeFile(t, dir, "good.md", markdownWithTitle("Good"))
	// No Space header and no --space, which fails in metadata rather than
	// anywhere that could be mistaken for a manifest problem.
	writeFile(t, dir, "bad.md", "# Orphan\n\nNo metadata at all.\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.ContinueOnError = true
	require.Error(t, Run(context.Background(), config), "the bad file should still fail the run")

	published, err := api.FindPage(context.Background(), "DOCS", "Good", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// The mapping for the file that did publish must have survived: retitle it
	// and it should be found again rather than duplicated.
	writeFile(t, dir, "good.md", markdownWithTitle("Good Renamed"))
	config.Files = good
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "Good Renamed", server.Page(published.ID).Title,
		"the mapping recorded on the failed run should have been saved")
	assert.Equal(t, 0, countPagesTitled(t, server, "Good"),
		"nothing should be left behind under the old title")
}

// TestTrackedPageLoadFailureDoesNotDuplicate covers the distinction flaw 6 was
// about. A recorded page that is genuinely gone falls through to being created
// again; any other failure to load it must stop the run instead, because
// treating a transient error as "the page is gone" publishes exactly the
// duplicate this feature exists to prevent.
func TestTrackedPageLoadFailureDoesNotDuplicate(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	published, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// The page is still there, but loading it fails the way a proxy or an
	// overloaded instance would.
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content/"+published.ID) {
			return http.StatusInternalServerError, `{"message":"upstream exploded"}`, true
		}
		return 0, "", false
	})

	// Retitle so resolution is forced through the manifest entry.
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc Renamed"))
	require.Error(t, Run(context.Background(), trackingConfig(server, file)),
		"a page that cannot be loaded must stop the run, not be republished")

	server.SetFail(nil)
	assert.Equal(t, 0, countPagesTitled(t, server, "Doc Renamed"),
		"no duplicate should have been created")
	assert.Equal(t, "Doc", server.Page(published.ID).Title,
		"the original page is untouched")
}

// TestTwoFilesClaimingOnePageIsReported: nothing stops two documents resolving
// to the same page by title, and then a rename of either moves a page the other
// also believes is its own. mark cannot tell which was meant, so it says so.
func TestTwoFilesClaimingOnePageIsReported(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// Same title, two files.
	writeFile(t, dir, "one.md", markdownWithTitle("Shared"))
	writeFile(t, dir, "two.md", markdownWithTitle("Shared"))

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	require.NoError(t, Run(context.Background(), trackingConfig(server, filepath.Join(dir, "*.md"))))

	assert.Contains(t, logged.String(), "both publish to page",
		"two files resolving to one page should be reported")
}

// TestPageIDModeSaysTrackingDoesNotApply: the mapping is per space and per
// file, and a publish straight to a page id has neither. Better said once than
// discovered later as a manifest with holes in it.
func TestPageIDModeSaysTrackingDoesNotApply(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	existing := server.AddPage("DOCS", "Existing", "page", "")
	file := writeFile(t, dir, "doc.md", "Just a body.\n")

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	config := trackingConfig(server, file)
	config.PageID = existing.ID
	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, logged.String(), "no effect together with a page id")
}

// TestDryRunWritesNothing pins the promise --dry-run makes. Tracking gave it a
// way to break that promise: resolving the hierarchy records what it finds, a
// recording marks the manifest dirty, and Save writes it -- so a dry run would
// modify Confluence.
func TestDryRunWritesNothing(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	config := trackingConfig(server, file)
	config.DryRun = true
	require.NoError(t, Run(context.Background(), config))

	for _, r := range server.Requests() {
		assert.Equal(t, http.MethodGet, r.Method,
			"a dry run must issue no writes, saw %s %s", r.Method, r.Path)
	}
}

// TestFolderPublishWithoutTracking is the path that carried a latent panic: a
// nil *manifest.Store placed in a FolderTracker interface is a non-nil
// interface, so with tracking off mark would have called through a nil
// receiver. Nothing exercised it, because the fake had no folder endpoints.
func TestFolderPublishWithoutTracking(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	config := trackingConfig(server, file)
	config.TrackPages = false

	require.NoError(t, Run(context.Background(), config), "publishing into a folder must work with tracking off")

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Equal(t, "Guides", folders[0].Title)
}

func TestFolderPublishWithTracking(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	folders := server.Folders()
	require.Len(t, folders, 1, "exactly one folder should exist")
	assert.Equal(t, "Guides", folders[0].Title)
}

// TestRenamedFolderIsFollowedNotDuplicated is what folder tracking is for.
// Folders are found by title, so one renamed in Confluence stops matching the
// header that declares it and mark builds a second beside the first, splitting
// the hierarchy.
func TestRenamedFolderIsFollowedNotDuplicated(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	original := server.Folders()
	require.Len(t, original, 1)

	// Somebody renames the folder in the Confluence UI. The header still says
	// "Guides".
	server.RenameFolder(original[0].ID, "Guides Renamed")

	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	after := server.Folders()
	assert.Len(t, after, 1,
		"the renamed folder should have been reused, not duplicated")
	assert.Equal(t, original[0].ID, after[0].ID)
}

// TestRenamedFolderDuplicatesWithoutTracking is the control: without the flag
// the old behaviour stands, which is what makes the test above mean anything.
func TestRenamedFolderDuplicatesWithoutTracking(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, "")
	config.TrackPages = false
	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	config.Files = file

	require.NoError(t, Run(context.Background(), config))
	original := server.Folders()
	require.Len(t, original, 1)

	server.RenameFolder(original[0].ID, "Guides Renamed")
	require.NoError(t, Run(context.Background(), config))

	assert.Len(t, server.Folders(), 2,
		"without tracking the renamed folder is not recognised and a second appears")
}

// TestFolderRepublishWithoutTrackingUpdatesInPlace publishes a document into a
// folder twice with tracking off. v1 reports no ancestors for a page whose
// parent is a folder, and the page found by title was read as one somewhere
// else in the space, so the second run created it again -- which Confluence
// refuses, a title being unique within a space.
func TestFolderRepublishWithoutTrackingUpdatesInPlace(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, "")
	config.TrackPages = false
	config.Files = writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))

	require.NoError(t, Run(context.Background(), config))
	require.NoError(t, Run(context.Background(), config), "the second run must update the page it published")

	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/api/v2/pages"),
		"the page is created once and updated after that")
}

// TestFolderDocumentLeavesHomePageAlone is the limit on the above: the home
// page has no ancestors either, and a document that happens to share its title
// must not move it into a folder.
func TestFolderDocumentLeavesHomePageAlone(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, "")
	config.TrackPages = false
	config.Files = writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Home"))

	// The create that follows fails on the duplicate title; what matters is
	// that nothing was moved on the way there.
	_ = Run(context.Background(), config)

	assert.Zero(t, server.CountRequests(http.MethodPut, "/move/"),
		"the home page must stay where it is")
}

// TestDryRunDoesNotRecordFolders covers the other defect the missing endpoints
// hid: resolving a hierarchy records what it finds, a recording marks the
// manifest dirty, and Save writes it -- so --dry-run wrote to Confluence.
func TestDryRunDoesNotRecordFolders(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// A folder that already exists, so a dry run resolves rather than creates.
	server.AddFolder("DOCS", "Guides", "", "page")

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	config := trackingConfig(server, file)
	config.DryRun = true
	require.NoError(t, Run(context.Background(), config))

	for _, r := range server.Requests() {
		assert.Equal(t, http.MethodGet, r.Method,
			"a dry run must issue no writes, saw %s %s", r.Method, r.Path)
	}
}

// markdownTitledFromFilename has no Title header, so its title comes from the
// filename -- which is what makes a rename change the title too, and the case
// that duplicates without rename detection.
func markdownTitledFromFilename() string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->

Body that does not change when the file moves.
`
}

func filenameTitleConfig(server *confluencetest.Server, files string) Config {
	config := trackingConfig(server, files)
	config.TitleFromFilename = true
	return config
}

// TestRenamedFileIsFollowedNotDuplicated is the last of the three rename causes
// asked for. The path is the manifest key, so renaming a file is a miss; and
// when the title comes from the filename the title lookup misses too, and mark
// publishes a second page. The only thing tying the old page to the new file is
// what the file contains.
func TestRenamedFileIsFollowedNotDuplicated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "old-name.md", markdownTitledFromFilename())
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	original, err := api.FindPage(context.Background(), "DOCS", "Old Name", "page")
	require.NoError(t, err)
	require.NotNil(t, original, "the first run publishes under the filename")

	// The file is renamed. Its content is untouched.
	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	assert.Equal(t, "New Name", server.Page(original.ID).Title,
		"the existing page should have been renamed, not left behind")
	assert.Equal(t, 0, countPagesTitled(t, server, "Old Name"),
		"nothing should remain under the old name")
}

// TestRenamedFileDuplicatesWithoutTracking is the control.
func TestRenamedFileDuplicatesWithoutTracking(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	config := filenameTitleConfig(server, filepath.Join(dir, "*.md"))
	config.TrackPages = false

	writeFile(t, dir, "old-name.md", markdownTitledFromFilename())
	require.NoError(t, Run(context.Background(), config))

	original, err := api.FindPage(context.Background(), "DOCS", "Old Name", "page")
	require.NoError(t, err)
	require.NotNil(t, original)

	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "Old Name", server.Page(original.ID).Title,
		"without tracking the old page is stranded under its old name")
	assert.Equal(t, 1, countPagesTitled(t, server, "New Name"),
		"and a second page appears")
}

// TestRenamedFileStopsBeingReportedAsDeleted: a followed rename must take the
// old path with it, or it is reported as a missing file on every run from then
// on.
func TestRenamedFileStopsBeingReportedAsDeleted(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "old-name.md", markdownTitledFromFilename())
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))
	assert.NotContains(t, logged.String(), "had no matching source file",
		"the old path should have been dropped when the rename was followed")
}

// TestAmbiguousRenameCreatesRatherThanGuesses: two unpublished documents with
// identical content give no basis for choosing between them. A duplicate is a
// nuisance; rebinding onto the wrong page overwrites something nobody asked to
// change.
func TestAmbiguousRenameCreatesRatherThanGuesses(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "one.md", markdownTitledFromFilename())
	writeFile(t, dir, "two.md", markdownTitledFromFilename())
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	// Both disappear and one appears with the same content.
	require.NoError(t, os.Remove(filepath.Join(dir, "one.md")))
	require.NoError(t, os.Rename(
		filepath.Join(dir, "two.md"), filepath.Join(dir, "three.md")))
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	assert.Equal(t, 1, countPagesTitled(t, server, "Three"),
		"an ambiguous match must create rather than rebind")
	assert.Equal(t, 1, countPagesTitled(t, server, "One"),
		"and must leave both candidates alone")
}

// TestDryRunReportsWhatWouldHappen: a dry run that announces a new page for a
// retitle the real run would have done in place is confidently wrong about the
// one thing somebody is checking.
func TestDryRunReportsWhatWouldHappen(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Original"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed"))

	config := trackingConfig(server, file)
	config.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(context.Background(), config)) })

	assert.Contains(t, logged, "retitled from",
		"a dry run should say the existing page would be retitled")
}

// TestDryRunStillWritesNothingWithTracking: the dry run now resolves through
// the manifest, which is what records folders, so the guard has to be that the
// store cannot write rather than that it is never asked to.
func TestDryRunStillWritesNothingWithTracking(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	// The folder has to hang off the anchor page the headers name, or it is not
	// resolved at all and nothing is ever recorded -- which would leave this
	// test asserting nothing.
	anchor, err := api.FindPage(context.Background(), "DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, anchor)
	server.AddFolder("DOCS", "Guides", anchor.ID, "page")

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))

	config := trackingConfig(server, file)
	config.DryRun = true
	require.NoError(t, Run(context.Background(), config))

	for _, r := range server.Requests() {
		assert.Equal(t, http.MethodGet, r.Method,
			"a dry run must issue no writes, saw %s %s", r.Method, r.Path)
	}
}

// multiGlobConfig is one of several mark invocations publishing different
// folders into a single space, which is a common way to run it.
func multiGlobConfig(server *confluencetest.Server, glob string) Config {
	return Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: glob, Features: []string{"mention"},
		TrackPages: true, TitleFromFilename: true, Output: io.Discard,
	}
}

// TestRenameDoesNotReachAcrossGlobs is the failure that scoping rename
// candidates prevents. Every path belonging to another invocation is missing
// from this one, so without the check they all look like deleted files this
// document might be a rename of -- and a new file sharing content with another
// invocation's document takes over its page, retitling it and dropping its
// entry, which leaves that document with no page and a duplicate on its next
// run.
func TestRenameDoesNotReachAcrossGlobs(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o750))
	globA := filepath.Join(dir, "a", "*.md")
	globB := filepath.Join(dir, "b", "*.md")

	// Identical content, which is all it takes: shared boilerplate or a copied
	// stub is enough.
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\nShared body text.\n"

	writeFile(t, dir, "a/one.md", body)
	require.NoError(t, Run(context.Background(), multiGlobConfig(server, globA)))

	pageA, err := api.FindPage(context.Background(), "DOCS", "One", "page")
	require.NoError(t, err)
	require.NotNil(t, pageA)

	// A second invocation adds a brand-new file that happens to match.
	writeFile(t, dir, "b/two.md", body)
	require.NoError(t, Run(context.Background(), multiGlobConfig(server, globB)))

	assert.Equal(t, "One", server.Page(pageA.ID).Title,
		"the other invocation's page must be left alone")
	assert.Equal(t, 1, countPagesTitled(t, server, "Two"),
		"the new file should have got its own page")

	// And the first invocation still owns its document: re-running it must not
	// republish, which is what would happen if its entry had been taken.
	require.NoError(t, Run(context.Background(), multiGlobConfig(server, globA)))
	assert.Equal(t, "One", server.Page(pageA.ID).Title)
	assert.Equal(t, 1, countPagesTitled(t, server, "One"),
		"no duplicate should have appeared for the first invocation")
}

// TestRenameStillWorksWithinOneGlob is the control: scoping must not cost the
// feature its point.
func TestRenameStillWorksWithinOneGlob(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o750))
	glob := filepath.Join(dir, "a", "*.md")
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\nBody.\n"

	writeFile(t, dir, "a/old-name.md", body)
	require.NoError(t, Run(context.Background(), multiGlobConfig(server, glob)))

	published, err := api.FindPage(context.Background(), "DOCS", "Old Name", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	require.NoError(t, os.Rename(
		filepath.Join(dir, "a", "old-name.md"), filepath.Join(dir, "a", "new-name.md")))
	require.NoError(t, Run(context.Background(), multiGlobConfig(server, glob)))

	assert.Equal(t, "New Name", server.Page(published.ID).Title,
		"a rename within one pattern must still be followed")
}

// TestTrackPagesThroughTheGatewayWithAManifestPage is issue #1020 end to end: a
// scoped API token cannot create a space property, so the mapping is kept on a
// page instead, through v2 -- and a rename on the second run still finds the
// page the first run made, with every v1 endpoint refused throughout.
func TestTrackPagesThroughTheGatewayWithAManifestPage(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/rest/api/") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		return 0, "", false
	})

	baseURL := server.URL + "/ex/confluence/cloud-id"
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: baseURL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, ManifestPage: "Home", Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config))

	api := confluence.NewAPI(baseURL, "user", "token", false)
	guide, err := api.FindPage(context.Background(), "DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, guide)

	assert.Equal(t, 0, server.CountRequests("POST", "/api/v2/spaces/"),
		"no space property was created")
	shards := 0
	for i := range manifest.ShardCount {
		if server.SpaceProperty(home.ID, manifest.PropertyKey(i)) != nil {
			shards++
		}
	}
	assert.Equal(t, 1, shards, "the mapping is on the page named")

	// A retitle: without the manifest this would be a second page.
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide Renamed -->\n\nA guide.\n")
	require.NoError(t, Run(context.Background(), config))

	renamed, err := api.FindPage(context.Background(), "DOCS", "Guide Renamed", "page")
	require.NoError(t, err)
	require.NotNil(t, renamed)
	assert.Equal(t, guide.ID, renamed.ID, "the manifest found the page the first run made")
}

// TestManifestPageByIDAcrossTwoSpaces: a page id names the same page for every
// space, so a run publishing to two of them used to read one manifest as both
// and write it back twice. The second write was refused as stale, warned about
// as a concurrent run, and the run exited 0 with that space's mapping gone.
// The run fails instead, and the space it did publish keeps its mapping.
func TestManifestPageByIDAcrossTwoSpaces(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	opsHome := server.AddPage("OPS", "Home", "page", "")
	server.SetHomepage("OPS", opsHome.ID)
	handbook := server.AddPage("DOCS", "Handbook", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "a.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")
	writeFile(t, dir, "b.md", "<!-- Space: OPS -->\n<!-- Title: Runbook -->\n\nA runbook.\n")

	err := Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, ManifestPage: handbook.ID, Output: io.Discard,
	})
	require.Error(t, err, "a run spanning two spaces on one manifest page fails")
	assert.Contains(t, err.Error(), "--manifest-page")
	assert.Contains(t, err.Error(), `"OPS"`)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	runbook, err := api.FindPage(context.Background(), "OPS", "Runbook", "page")
	require.NoError(t, err)
	assert.Nil(t, runbook, "refused before anything was published to the second space")

	assert.NotNil(t,
		server.SpaceProperty(handbook.ID, manifest.PropertyKey(manifest.ShardFor(filepath.ToSlash(filepath.Join(dir, "a.md"))))),
		"the first space's mapping is saved")
}

// TestManifestPageRequiresTrackPages: the flag says where a manifest is kept,
// and nothing else keeps one.
func TestManifestPageRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	err := Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), ManifestPage: "Home", Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--manifest-page requires --track-pages")
}

// TestManifestPrefixRequiresTrackPages: the flag names the manifest's
// properties, and nothing else keeps one. The default value is not a request.
func TestManifestPrefixRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		ManifestPrefix: "mark.manifest", Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config), "the default prefix asks for nothing")

	config.ManifestPrefix = "team.docs"
	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--manifest-prefix requires --track-pages")

	config.TrackPages = true
	config.ManifestPrefix = "not a key"
	err = Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot prefix a property key")
}

// TestManifestIsSavedWhenAFileFailsHard: the mapping records pages that have
// already been published. Returning on the first bad file skipped the save
// entirely, so one malformed document threw away the identity of every page
// that had published before it -- and the next run resolved all of them by
// title alone, with no rename detection and no --no-overwrite baseline.
//
// The sibling case, --continue-on-error, was fixed long ago and its comment
// explains exactly why. This is the path that comment did not cover.
func TestManifestIsSavedWhenAFileFailsHard(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	// Named so the good one is processed first and the run then fails.
	writeFile(t, dir, "a-good.md", markdownWithTitle("Published"))
	writeFile(t, dir, "b-bad.md", "no metadata here at all\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.Error(t, Run(context.Background(), config), "the run must still report the failure")

	published, err := api.FindPage(context.Background(), "DOCS", "Published", "page")
	require.NoError(t, err)
	require.NotNil(t, published, "the good file published")

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup(context.Background(), "DOCS", filepath.Join(dir, "a-good.md"))
	require.NoError(t, err)
	require.True(t, ok, "the page that published must still be in the manifest")
	assert.Equal(t, published.ID, entry.PageID)
}

// TestManifestSaveOnTheWayOutOutlastsASignal: the save on the way out of a
// run that failed went out under the run's own context whenever that context
// was still live, so a signal arriving during the save cut it short with the
// manifest half-written. It is made under a detached context whatever the
// state of the run's.
func TestManifestSaveOnTheWayOutOutlastsASignal(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-good.md", markdownWithTitle("Published"))
	writeFile(t, dir, "b-bad.md", "no metadata here at all\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The save's first write stops the run, as a signal would, and is then
	// refused if the client gives up on it; a save that does not give up is
	// let through.
	var once sync.Once
	signalled := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet || !strings.Contains(r.URL.Path, "/propert") {
			return 0, "", false
		}

		aborted := false
		once.Do(func() {
			signalled = true
			cancel()
			select {
			case <-r.Context().Done():
				aborted = true
			case <-time.After(500 * time.Millisecond):
			}
		})
		if aborted {
			return http.StatusServiceUnavailable, `{"message":"the client gave up"}`, true
		}

		return 0, "", false
	})

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.Error(t, Run(ctx, config), "the run must still report the failure")
	require.True(t, signalled, "the save on the way out wrote a property")

	server.SetFail(nil)

	published, err := api.FindPage(context.Background(), "DOCS", "Published", "page")
	require.NoError(t, err)
	require.NotNil(t, published, "the good file published")

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup(context.Background(), "DOCS", filepath.Join(dir, "a-good.md"))
	require.NoError(t, err)
	require.True(t, ok, "the save went on to the end despite the signal")
	assert.Equal(t, published.ID, entry.PageID)
}

// TestNothingIsRecordedForAPageThatWasNotWritten: the mapping asserts what is on
// the page. Recording it before the upload, now that the manifest is saved
// however the run ends, meant a body refused as malformed still left the
// manifest claiming the new title and a fingerprint of the source that produced
// it -- and the fingerprint is what rename detection matches on, so the next run
// read a document it had never published as unchanged.
func TestNothingIsRecordedForAPageThatWasNotWritten(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// An unbalanced tag the author wrote: the well-formedness gate refuses the
	// body, so nothing is uploaded.
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Refused -->

Text with an <span>unclosed tag.
`)

	require.Error(t, Run(context.Background(), trackingConfig(server, file)))

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	_, ok, err := store.Lookup(context.Background(), "DOCS", filepath.Join(dir, "doc.md"))
	require.NoError(t, err)
	assert.False(t, ok,
		"a page whose body was never uploaded must not be in the manifest")
}

// TestRecordedTitleIsTheOneThePageCarries covers the same ordering from the
// other side: a successful publish still has to record, or tracking stops
// working altogether.
func TestRecordedTitleIsTheOneThePageCarries(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Recorded"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	published, err := api.FindPage(context.Background(), "DOCS", "Recorded", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	store := manifest.NewStore(confluence.NewAPI(server.URL, "user", "token", false))
	entry, ok, err := store.Lookup(context.Background(), "DOCS", filepath.Join(dir, "doc.md"))
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, published.ID, entry.PageID)
	assert.Equal(t, "Recorded", entry.Title)
	assert.NotEmpty(t, entry.Hash, "the fingerprint is what a rename is matched on")
}

// TestPreserveCommentsKeepsARetitle: the new title is staged on the page object
// and published by the update. --preserve-comments refetches the page in
// between, which brought back the title Confluence still held, and the rename
// was dropped without a word.
//
// It compounded: the manifest went on to record the new title against a page
// carrying the old one, so every parent named by that title resolved to the
// wrong place from then on, and under --changes-only the page was stuck under
// its old name for good.
func TestPreserveCommentsKeepsARetitle(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Original Title")))
	config.PreserveComments = true
	require.NoError(t, Run(context.Background(), config))

	original, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage(context.Background(), "DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, original)

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, "Renamed Title", server.Page(original.ID).Title,
		"the page should have been renamed in place")
	assert.Equal(t, 0, countPagesTitled(t, server, "Original Title"))
}

// TestPreserveCommentsLeavesAnUnchangedTitleAlone is the control: the fix has
// to restore the title that was staged, not stamp the document's title onto
// every page it touches.
func TestPreserveCommentsLeavesAnUnchangedTitleAlone(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	config := trackingConfig(server, writeFile(t, dir, "doc.md", markdownWithTitle("Steady")))
	config.PreserveComments = true

	require.NoError(t, Run(context.Background(), config))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, 1, countPagesTitled(t, server, "Steady"))
}

// TestGeneratedHashDoesNotStandInForATitle: a document with no title of any
// kind, under --title-append-generated-hash, was published as "- a159f529"
// instead of failing on the missing title.
func TestGeneratedHashDoesNotStandInForATitle(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\nbody\n")

	err := Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
		TitleAppendGeneratedHash: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page title is not set")
}
