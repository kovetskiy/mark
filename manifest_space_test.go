package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoSpaces is DOCS, which the documents publish to, and OTHER, holding the
// one page Elsewhere that no document names. It returns what holds DOCS's page
// manifest: the space on Cloud, its homepage on Data Center.
func twoSpaces(t *testing.T) (*confluencetest.Server, string, *confluencetest.Page) {
	t.Helper()

	return twoSpacesOn(t, false)
}

func twoSpacesOn(t *testing.T, dataCenter bool) (*confluencetest.Server, string, *confluencetest.Page) {
	t.Helper()

	server := confluencetest.New(t)
	if dataCenter {
		server.SetDataCenter()
	}
	docs := server.AddSpace("DOCS")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	otherHome := server.AddPage("OTHER", "Other Home", "page", "")
	server.SetHomepage("OTHER", otherHome.ID)
	elsewhere := server.AddPage("OTHER", "Elsewhere", "page", otherHome.ID)
	server.WritePage(elsewhere.ID, "<p>Elsewhere.</p>")

	owner := docs.ID
	if dataCenter {
		owner = home.ID
	}

	return server, owner, server.Page(elsewhere.ID)
}

// repointEntries rewrites every page id in a manifest property that from
// names to to, as an entry pointed at a page in another space would read.
func repointEntries(t *testing.T, server *confluencetest.Server, ownerID, key, from, to string) {
	t.Helper()

	property := server.SpaceProperty(ownerID, key)
	require.NotNil(t, property, "property %s", key)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(property.Value, &doc))

	changed := 0
	for _, field := range []string{"pages", "parents", "folders"} {
		entries, _ := doc[field].(map[string]any)
		for name, value := range entries {
			switch entry := value.(type) {
			case string:
				if entry == from {
					entries[name] = to
					changed++
				}
			case map[string]any:
				if entry["pageId"] == from {
					entry["pageId"] = to
					changed++
				}
			}
		}
	}
	require.Positive(t, changed, "no entry of %s names %s", key, from)

	value, err := json.Marshal(doc)
	require.NoError(t, err)
	server.SetSpaceProperty(ownerID, key, value)
}

// assertUntouched checks that nothing became of the page in OTHER.
func assertUntouched(t *testing.T, server *confluencetest.Server, before *confluencetest.Page) {
	t.Helper()

	after := server.Page(before.ID)
	require.NotNil(t, after)
	assert.Equal(t, before.Title, after.Title)
	assert.Equal(t, before.Version, after.Version)
	assert.Equal(t, before.Body, after.Body)
	assert.Equal(t, before.ParentID, after.ParentID)
	assert.Equal(t, "OTHER", after.SpaceKey)
	assert.False(t, after.Trashed)
	assert.False(t, after.Archived)
}

// publishJSON runs config and returns its JSON report.
func publishJSON(t *testing.T, config Config) string {
	t.Helper()

	var out bytes.Buffer
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out
	require.NoError(t, Run(context.Background(), config))

	return out.String()
}

// warningsOf decodes a JSON report and returns the warnings of one kind.
func warningsOf(t *testing.T, out string, kind report.WarningKind) []report.Warning {
	t.Helper()

	var got report.Report
	require.NoError(t, json.Unmarshal([]byte(out), &got))

	var warnings []report.Warning
	for _, pg := range got.Pages {
		for _, warning := range pg.WarningDetails {
			if warning.Kind == kind {
				warnings = append(warnings, warning)
			}
		}
	}

	return warnings
}

// TestEntryInAnotherSpaceIsNotPublishedTo: a document whose title lookup
// misses, and whose entry names a page in another space, is published by title
// in its own space, and the page elsewhere is left as it was.
func TestEntryInAnotherSpaceIsNotPublishedTo(t *testing.T) {
	for _, platform := range []string{"cloud", "data center"} {
		t.Run(platform, func(t *testing.T) {
			dataCenter := platform == "data center"
			server, docsID, elsewhere := twoSpacesOn(t, dataCenter)
			dir := t.TempDir()

			file := writeFile(t, dir, "doc.md", markdownWithTitle("Original Title"))
			require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

			original := pageIDTitled(t, server, "Original Title")
			require.NotEmpty(t, original)
			repointEntries(t, server, docsID,
				manifest.PropertyKey(manifest.ShardFor(filepath.ToSlash(file))), original, elsewhere.ID)

			writeFile(t, dir, "doc.md", markdownWithTitle("Renamed Title"))

			// --changes-only, so that the dry run writes its report rather than the page.
			previewConfig := trackingConfig(server, file)
			previewConfig.Update.ChangesOnly = true
			preview := warningsOf(t, previewOf(t, previewConfig, report.FormatJSON), report.WarningOtherSpace)
			assertUntouched(t, server, elsewhere)

			out := publishJSON(t, trackingConfig(server, file))
			assertUntouched(t, server, elsewhere)
			assert.NotEmpty(t, pageIDTitled(t, server, "Renamed Title"), "published by title in DOCS")

			warnings := warningsOf(t, out, report.WarningOtherSpace)
			require.Len(t, warnings, 1)
			assert.Equal(t, file, warnings[0].File)
			assert.Contains(t, warnings[0].Message, elsewhere.ID)
			assert.Contains(t, warnings[0].Message, `space "OTHER"`)
			require.Len(t, preview, 1, "a dry run warns as the real run does")
			assert.Equal(t, warnings[0], preview[0])
		})
	}
}

// TestRenameIsNotFollowedIntoAnotherSpace: a renamed file whose old path's
// entry names a page in another space is published by title, not treated as a
// rename of that page.
func TestRenameIsNotFollowedIntoAnotherSpace(t *testing.T) {
	server, docsID, elsewhere := twoSpaces(t)
	dir := t.TempDir()

	old := writeFile(t, dir, "old-name.md", markdownTitledFromFilename())
	glob := filepath.Join(dir, "*.md")
	require.NoError(t, Run(context.Background(), filenameTitleConfig(server, glob)))

	original := pageIDTitled(t, server, "Old Name")
	require.NotEmpty(t, original)
	repointEntries(t, server, docsID,
		manifest.PropertyKey(manifest.ShardFor(filepath.ToSlash(old))), original, elsewhere.ID)

	require.NoError(t, os.Rename(old, filepath.Join(dir, "new-name.md")))

	config := filenameTitleConfig(server, glob)
	config.Tracking.OnOrphan = report.OnOrphanDelete
	out := publishJSON(t, config)
	assertUntouched(t, server, elsewhere)
	assert.NotEmpty(t, pageIDTitled(t, server, "New Name"), "published by title in DOCS")

	warnings := warningsOf(t, out, report.WarningOtherSpace)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, old)
	assert.Contains(t, warnings[0].Message, elsewhere.ID)
}

// TestOnOrphanLeavesAPageInAnotherSpace: an orphan whose entry names a page in
// another space is neither archived nor deleted, is reported as left alone,
// and stays recorded; an orphan in the manifest's own space still is removed.
// Archiving runs on Data Center, deleting on Cloud.
func TestOnOrphanLeavesAPageInAnotherSpace(t *testing.T) {
	for _, action := range []report.OnOrphan{report.OnOrphanDelete, report.OnOrphanArchive} {
		t.Run(string(action), func(t *testing.T) {
			server, docsID, elsewhere := twoSpacesOn(t, action == report.OnOrphanArchive)
			dir := t.TempDir()

			writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))
			moved := writeFile(t, dir, "moved.md", markdownWithTitle("Moved"))
			writeFile(t, dir, "gone.md", markdownWithTitle("Gone"))

			config := trackingConfig(server, filepath.Join(dir, "*.md"))
			config.Tracking.OnOrphan = action
			require.NoError(t, Run(context.Background(), config))

			movedID := pageIDTitled(t, server, "Moved")
			goneID := pageIDTitled(t, server, "Gone")
			require.NotEmpty(t, movedID)
			require.NotEmpty(t, goneID)
			repointEntries(t, server, docsID,
				manifest.PropertyKey(manifest.ShardFor(filepath.ToSlash(moved))), movedID, elsewhere.ID)

			require.NoError(t, os.Remove(moved))
			require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

			logged := captureLog(t, func() {
				previewOf(t, config, report.FormatJSON)
			})
			assert.Contains(t, logged, `page \"Moved\" (`+elsewhere.ID+`) is left alone`)
			assert.NotContains(t, logged, `page \"Moved\" would be`)
			assert.Contains(t, logged, `page \"Gone\" would be `+string(action)+`d`)
			assertUntouched(t, server, elsewhere)

			for range 2 {
				var got report.Report
				require.NoError(t, json.Unmarshal([]byte(publishJSON(t, config)), &got))

				assertUntouched(t, server, elsewhere)

				var kept *report.Orphan
				for i := range got.Orphans {
					if got.Orphans[i].PageID == elsewhere.ID {
						kept = &got.Orphans[i]
					}
				}
				require.NotNil(t, kept, "the entry stays recorded and is reported on every run")
				assert.Equal(t, moved, kept.File)
				assert.Equal(t, report.OnOrphanReport, kept.Action)
				assert.Equal(t, `it is in space "OTHER", not space "DOCS", whose page manifest records it`, kept.Reason)
			}

			gone := server.Page(goneID)
			if action == report.OnOrphanDelete {
				assert.True(t, gone.Trashed, "an orphan in DOCS is still removed")
			} else {
				assert.True(t, gone.Archived, "an orphan in DOCS is still removed")
			}
		})
	}
}

// TestRenamedParentIsNotFollowedIntoAnotherSpace: a declared parent whose
// recorded page is in another space keeps the title as written.
func TestRenamedParentIsNotFollowedIntoAnotherSpace(t *testing.T) {
	server, docsID, elsewhere := twoSpaces(t)
	dir := t.TempDir()

	guide := handMadeParent(t, server, "Guide")
	file := writeFile(t, dir, "child.md", markdownUnder("Guide", "Child"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	server.RenamePage(guide.ID, "Guide Renamed")
	repointEntries(t, server, docsID, manifest.ParentPropertyKey, guide.ID, elsewhere.ID)

	out := publishJSON(t, trackingConfig(server, file))
	assertUntouched(t, server, elsewhere)
	assert.Zero(t, countPagesTitled(t, server, "Elsewhere"), "the other space's title is not taken up in DOCS")
	assert.NotEmpty(t, pageIDTitled(t, server, "Guide"), "the parent is found or made by the title written")

	warnings := warningsOf(t, out, report.WarningOtherSpace)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, `parent "Guide"`)
	assert.Contains(t, warnings[0].Message, elsewhere.ID)
}

// TestRenamedFolderIsNotFollowedIntoAnotherSpace: a folder whose recorded id
// is a folder in another space is not used as the parent.
func TestRenamedFolderIsNotFollowedIntoAnotherSpace(t *testing.T) {
	server, docsID, elsewhere := twoSpaces(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownInFolder("Guides", "Doc"))
	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	folders := server.Folders()
	require.Len(t, folders, 1)
	server.RenameFolder(folders[0].ID, "Guides Renamed")

	away := server.AddFolder("OTHER", "Away", elsewhere.ParentID, "page")
	repointEntries(t, server, docsID, manifest.FolderPropertyKey, folders[0].ID, away.ID)

	out := publishJSON(t, trackingConfig(server, file))

	doc := pageIDTitled(t, server, "Doc")
	require.NotEmpty(t, doc)
	assert.Equal(t, "DOCS", server.Page(doc).SpaceKey)
	assert.NotEqual(t, away.ID, server.Page(doc).ParentID)
	assert.Equal(t, "Away", server.Folder(away.ID).Title)

	warnings := warningsOf(t, out, report.WarningOtherSpace)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, `folder "Guides"`)
	assert.Contains(t, warnings[0].Message, away.ID)
}
