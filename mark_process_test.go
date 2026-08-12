package mark

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile writes content into dir and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestProcessFileFetchesAttachmentsOnce pins the fix for the duplicate
// GetAttachments. ProcessFile resolves attachments twice per page -- declared
// attachments, then diagrams found while rendering -- and each pass used to
// issue its own paginated fetch of the same remote list.
func TestProcessFileFetchesAttachmentsOnce(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	// A 1x1 PNG so the image-dimension probe has something valid to read.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o600))

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Attachment Doc -->
<!-- Attachment: logo.png -->

# Attachment Doc

![logo](logo.png)
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	server.ResetRequests()

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	assert.Equal(t, 1, server.CountRequests("GET", "/child/attachment"),
		"the remote attachment list should be fetched once per page")

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
}

// TestProcessFileCreatesAndUpdatesPage is a smoke test for the whole
// orchestration path, which had no coverage at all: metadata, ancestry, page
// creation, body upload and labels in one run.
func TestProcessFileCreatesAndUpdatesPage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: New Page -->
<!-- Label: alpha -->

# New Page

Some **content**.
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	stored := server.Page(target.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "New Page", stored.Title)
	assert.Contains(t, stored.Body, "<strong>content</strong>")
	assert.Equal(t, []string{"alpha"}, stored.Labels)

	// The page was created under the declared parent.
	parent, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	assert.Equal(t, parent.ID, stored.ParentID)
}

// markdownWithTitle is the document under test, parameterised on its title.
func markdownWithTitle(title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: ` + title + ` -->

# ` + title + `

Some content.
`
}

// trackingConfig is a publish configured to remember page identity.
func trackingConfig(server *confluencetest.Server, files string) Config {
	return Config{
		BaseURL:    server.URL,
		Username:   "user",
		Password:   "token",
		Files:      files,
		Features:   []string{"mention"},
		TrackPages: true,
		Output:     io.Discard,
	}
}

// docsSpace builds the minimal realistic tree the publish path expects.
func docsSpace(t *testing.T) (*confluencetest.Server, *confluence.API) {
	t.Helper()
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	return server, api
}

// countPagesTitled reports whether any page in the space carries a title, which
// is what tells a retitle apart from a duplicate.
//
// It builds its own client on purpose. confluence.API memoises FindPage by
// (space, title, type), so asking a client that has already looked this title up
// would be answered from that cache and report a page that has since been
// renamed out from under it.
func countPagesTitled(t *testing.T, server *confluencetest.Server, title string) int {
	t.Helper()
	fresh := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := fresh.FindPage("DOCS", title, "page")
	require.NoError(t, err)
	if page == nil {
		return 0
	}
	return 1
}

// TestTrackedPageIsRetitledNotDuplicated is the behaviour the manifest exists
// for. mark finds a page by title, so changing the title makes the existing page
// unfindable and a second one is published beside it. With tracking on, the file
// path resolves to the page that was published last time and it is renamed in
// place.
func TestTrackedPageIsRetitledNotDuplicated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Original Title"))
	require.NoError(t, Run(trackingConfig(server, file)))

	before, err := api.FindPage("DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	// The Title header changes; the file does not move.
	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(trackingConfig(server, file)))

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
	require.NoError(t, Run(config))

	before, err := api.FindPage("DOCS", "Original Title", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))
	require.NoError(t, Run(config))

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
	require.NoError(t, Run(config))

	before, err := api.FindPage("DOCS", "First", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	// Only the title changes; the rendered body is byte-identical.
	writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Second"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Second", server.Page(before.ID).Title,
		"a retitle must not be skipped just because the content hash matched")
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

// TestTrackedPageDeletedInConfluenceIsRecreated: someone removed the page from
// Confluence by hand. The manifest still names it, and pointing at a page that
// is not there must not wedge the run.
func TestTrackedPageDeletedInConfluenceIsRecreated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))
	require.NoError(t, Run(trackingConfig(server, file)))

	published, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	server.DeletePage(published.ID)

	// Retitle too, so the title lookup also misses and resolution is forced
	// through the manifest entry that now dangles.
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc Renamed"))
	require.NoError(t, Run(trackingConfig(server, file)),
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
	require.Error(t, Run(config), "the bad file should still fail the run")

	published, err := api.FindPage("DOCS", "Good", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// The mapping for the file that did publish must have survived: retitle it
	// and it should be found again rather than duplicated.
	writeFile(t, dir, "good.md", markdownWithTitle("Good Renamed"))
	config.Files = good
	require.NoError(t, Run(config))

	assert.Equal(t, "Good Renamed", server.Page(published.ID).Title,
		"the mapping recorded on the failed run should have been saved")
	assert.Equal(t, 0, countPagesTitled(t, server, "Good"),
		"nothing should be left behind under the old title")
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

// TestTrackedPageLoadFailureDoesNotDuplicate covers the distinction flaw 6 was
// about. A recorded page that is genuinely gone falls through to being created
// again; any other failure to load it must stop the run instead, because
// treating a transient error as "the page is gone" publishes exactly the
// duplicate this feature exists to prevent.
func TestTrackedPageLoadFailureDoesNotDuplicate(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))
	require.NoError(t, Run(trackingConfig(server, file)))

	published, err := api.FindPage("DOCS", "Doc", "page")
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
	require.Error(t, Run(trackingConfig(server, file)),
		"a page that cannot be loaded must stop the run, not be republished")

	server.SetFail(nil)
	assert.Equal(t, 0, countPagesTitled(t, server, "Doc Renamed"),
		"no duplicate should have been created")
	assert.Equal(t, "Doc", server.Page(published.ID).Title,
		"the original page is untouched")
}

// markdownUnder is a document declaring the full ancestry Parent > parent.
// The whole chain has to be declared, not just the immediate parent, or mark
// rejects the page as sitting at a different depth than its headers claim.
func markdownUnder(parent, title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Parent: ` + parent + ` -->
<!-- Title: ` + title + ` -->

Body.
`
}

// TestRenamedParentIsFollowedNotRecreated is the reference half of the problem
// tracking exists for. mark names a parent by title and creates it when the
// title is not found, so renaming a page that other documents declare as their
// parent used to strand them under a fresh empty page carrying the old name --
// and tracking made that likelier, because the parent is now renamed in place
// rather than duplicated somewhere visible.
func TestRenamedParentIsFollowedNotRecreated(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	// Named so the parent is processed first. With the child first it is
	// resolved while the parent still carries its old title, and the stale
	// reference is never exercised -- which is exactly how this test passed
	// against the unfixed code the first time it was written.
	writeFile(t, dir, "a-parent.md", markdownWithTitle("Guide"))
	writeFile(t, dir, "b-child.md", markdownUnder("Guide", "Child"))
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(trackingConfig(server, glob)))

	parentPage, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, parentPage)

	// The parent is renamed. The child still declares the old title.
	writeFile(t, dir, "a-parent.md", markdownWithTitle("Guide Renamed"))
	require.NoError(t, Run(trackingConfig(server, glob)))

	assert.Equal(t, "Guide Renamed", server.Page(parentPage.ID).Title,
		"the parent should have been renamed in place")
	assert.Equal(t, 0, countPagesTitled(t, server, "Guide"),
		"no empty page should have been created under the old title")

	child, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.Equal(t, parentPage.ID, server.Page(child.ID).ParentID,
		"the child should still sit under the same page it always did")
}

// TestUnknownParentTitleIsStillCreated is the control: a parent mark has never
// published is not a stale reference, and must still be created as before.
func TestUnknownParentTitleIsStillCreated(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "child.md", markdownUnder("Brand New Parent", "Child"))
	require.NoError(t, Run(trackingConfig(server, file)))

	assert.Equal(t, 1, countPagesTitled(t, server, "Brand New Parent"),
		"an unknown parent title is not stale and should still be created")
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

	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

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
	require.NoError(t, Run(config))

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
	require.NoError(t, Run(config))

	for _, r := range server.Requests() {
		assert.Equal(t, http.MethodGet, r.Method,
			"a dry run must issue no writes, saw %s %s", r.Method, r.Path)
	}
}

// markdownInFolder declares a folder hierarchy under the MARK_PARENTS anchor.
func markdownInFolder(folder, title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Folder: ` + folder + ` -->
<!-- Title: ` + title + ` -->

Body.
`
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

	require.NoError(t, Run(config), "publishing into a folder must work with tracking off")

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Equal(t, "Guides", folders[0].Title)
}

func TestFolderPublishWithTracking(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	require.NoError(t, Run(trackingConfig(server, file)))

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
	require.NoError(t, Run(trackingConfig(server, file)))

	original := server.Folders()
	require.Len(t, original, 1)

	// Somebody renames the folder in the Confluence UI. The header still says
	// "Guides".
	server.RenameFolder(original[0].ID, "Guides Renamed")

	require.NoError(t, Run(trackingConfig(server, file)))

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

	require.NoError(t, Run(config))
	original := server.Folders()
	require.Len(t, original, 1)

	server.RenameFolder(original[0].ID, "Guides Renamed")
	require.NoError(t, Run(config))

	assert.Len(t, server.Folders(), 2,
		"without tracking the renamed folder is not recognised and a second appears")
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
	require.NoError(t, Run(config))

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
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

	original, err := api.FindPage("DOCS", "Old Name", "page")
	require.NoError(t, err)
	require.NotNil(t, original, "the first run publishes under the filename")

	// The file is renamed. Its content is untouched.
	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

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
	require.NoError(t, Run(config))

	original, err := api.FindPage("DOCS", "Old Name", "page")
	require.NoError(t, err)
	require.NotNil(t, original)

	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(config))

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
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

	require.NoError(t, os.Rename(
		filepath.Join(dir, "old-name.md"), filepath.Join(dir, "new-name.md")))
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	require.NoError(t, Run(filenameTitleConfig(server, glob)))
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
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

	// Both disappear and one appears with the same content.
	require.NoError(t, os.Remove(filepath.Join(dir, "one.md")))
	require.NoError(t, os.Rename(
		filepath.Join(dir, "two.md"), filepath.Join(dir, "three.md")))
	require.NoError(t, Run(filenameTitleConfig(server, glob)))

	assert.Equal(t, 1, countPagesTitled(t, server, "Three"),
		"an ambiguous match must create rather than rebind")
	assert.Equal(t, 1, countPagesTitled(t, server, "One"),
		"and must leave both candidates alone")
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

// captureLog runs fn with the global logger redirected, returning what it wrote.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&buf)
	defer func() { log.Logger = restore }()
	fn()
	return buf.String()
}

// TestDryRunReportsWhatWouldHappen: a dry run that announces a new page for a
// retitle the real run would have done in place is confidently wrong about the
// one thing somebody is checking.
func TestDryRunReportsWhatWouldHappen(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Original"))
	require.NoError(t, Run(trackingConfig(server, file)))

	writeFile(t, dir, "doc.md", markdownWithTitle("Renamed"))

	config := trackingConfig(server, file)
	config.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(config)) })

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
	anchor, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, anchor)
	server.AddFolder("DOCS", "Guides", anchor.ID, "page")

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))

	config := trackingConfig(server, file)
	config.DryRun = true
	require.NoError(t, Run(config))

	for _, r := range server.Requests() {
		assert.Equal(t, http.MethodGet, r.Method,
			"a dry run must issue no writes, saw %s %s", r.Method, r.Path)
	}
}

// markdownUnderParent declares a single named parent.
func markdownUnderParent(parent, title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: ` + parent + ` -->
<!-- Title: ` + title + ` -->

Body.
`
}

// TestChangingTheParentHeaderMovesThePage is issue #430. Changing a Parent
// header used to fail the run with an ancestry error, leaving the declared
// hierarchy and the real one disagreeing and doing nothing about either. The
// reporter said plainly that they expected the page to be moved.
func TestChangingTheParentHeaderMovesThePage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	other := server.AddPage("DOCS", "Other", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownUnderParent("Parent", "Doc"))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	published, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// The document now declares a different parent.
	writeFile(t, dir, "doc.md", markdownUnderParent("Other", "Doc"))
	require.NoError(t, Run(config), "a changed parent should move the page, not fail the run")

	assert.Equal(t, other.ID, server.Page(published.ID).ParentID,
		"the page should now sit under the parent its headers name")
}

// TestDeeperNestingThanDeclaredIsLeftAlone is the guard on the above. A page
// nested below its declared parent passes validation today -- every declared
// parent is somewhere in its ancestry -- and moving those would tear up
// hierarchies nobody asked to change.
func TestDeeperNestingThanDeclaredIsLeftAlone(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	parent := server.AddPage("DOCS", "Parent", "page", home.ID)
	extra := server.AddPage("DOCS", "Extra", "page", parent.ID)
	deep := server.AddPage("DOCS", "Doc", "page", extra.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownUnderParent("Parent", "Doc"))
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}))

	assert.Equal(t, extra.ID, server.Page(deep.ID).ParentID,
		"a page nested deeper than declared must stay where it is")
	_ = api
}

// TestDryRunDoesNotMoveThePage: a dry run reports, it does not rearrange.
func TestDryRunDoesNotMoveThePage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	parent := server.AddPage("DOCS", "Parent", "page", home.ID)
	server.AddPage("DOCS", "Other", "page", home.ID)
	existing := server.AddPage("DOCS", "Doc", "page", parent.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownUnderParent("Other", "Doc"))
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, DryRun: true, Output: io.Discard,
	}))

	assert.Equal(t, parent.ID, server.Page(existing.ID).ParentID,
		"a dry run must leave the hierarchy exactly as it found it")
	_ = api
}

// TestTitleAndParentChangingTogetherMovesThePage covers the seam between
// tracking and relocation. A page resolved through the manifest never passed
// the ancestry check -- that check starts from a title lookup which, the title
// having changed, just missed -- so an edit changing both would retitle the page
// and quietly leave it where it was.
func TestTitleAndParentChangingTogetherMovesThePage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	other := server.AddPage("DOCS", "Other", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownUnderParent("Parent", "Doc"))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	published, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// One edit, both changes.
	writeFile(t, dir, "doc.md", markdownUnderParent("Other", "Doc Renamed"))
	require.NoError(t, Run(config))

	after := server.Page(published.ID)
	require.NotNil(t, after)
	assert.Equal(t, "Doc Renamed", after.Title, "the page should have been retitled")
	assert.Equal(t, other.ID, after.ParentID, "and moved to the parent its headers name")
}

// TestTrackedPageDeeperThanDeclaredIsLeftAlone is the same guard as for the
// title-lookup path, on the manifest path: nesting below the declared parent
// contradicts nothing and must not be rearranged.
func TestTrackedPageDeeperThanDeclaredIsLeftAlone(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	parent := server.AddPage("DOCS", "Parent", "page", home.ID)
	extra := server.AddPage("DOCS", "Extra", "page", parent.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownUnderParent("Parent", "Doc"))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	published, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// Somebody nests it one level deeper, which the headers do not contradict.
	require.NoError(t, api.MoveContentAppend(published.ID, extra.ID))

	writeFile(t, dir, "doc.md", markdownUnderParent("Parent", "Doc Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, extra.ID, server.Page(published.ID).ParentID,
		"a tracked page nested deeper than declared must stay where it is")
}
