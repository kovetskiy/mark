package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func childDoc(parent string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: " + parent + " -->\n<!-- Title: Child -->\n\n# Child\n\nChild body.\n"
}

// TestRetitledParentAndChildInOneRun: a document and the child naming it as
// parent are retitled together, and the child is processed first. The child's
// parent title exists nowhere yet, so a page was created under it, and the
// parent document then found that page by title and published there instead of
// renaming the page it tracks -- leaving the original empty and every child
// moved.
func TestRetitledParentAndChildInOneRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)
	require.NotEmpty(t, childID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	writeFile(t, dir, "a-child.md", childDoc("Parent Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, parentID, pageIDTitled(t, server, "Parent Renamed"), "the tracked page should be renamed in place")
	assert.Empty(t, pageIDTitled(t, server, "Parent"), "the old title should be gone")
	assert.Equal(t, childID, pageIDTitled(t, server, "Child"))
	assert.Equal(t, parentID, server.Page(childID).ParentID, "the child should stay under the original page")
}

// TestRetitledParentNamedWithoutRegardToCase: the child spells the new parent
// title in another case, which Confluence reads as the same title.
func TestRetitledParentNamedWithoutRegardToCase(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	writeFile(t, dir, "a-child.md", childDoc("parent renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Parent Renamed", server.Page(parentID).Title)
	assert.Equal(t, parentID, server.Page(childID).ParentID)
}

// TestChildNamingOldParentTitleStillFollowsRename keeps refreshStaleParents
// working: only the parent is retitled, the child still names the old title.
func TestChildNamingOldParentTitleStillFollowsRename(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	require.NotEmpty(t, parentID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Parent Renamed", server.Page(parentID).Title)
	assert.Empty(t, pageIDTitled(t, server, "Parent"))
}

// TestRetitledParentAndChildWithGeneratedHash: under
// --title-append-generated-hash a page is published as "Title - <hash>", and the
// child names the parent by that title. The run's index has to hold the hashed
// title too, with and without a path hierarchy, or the child sorting first
// creates an empty page under the new name.
func TestRetitledParentAndChildWithGeneratedHash(t *testing.T) {
	hashed := func(title string) string {
		return metadata.GeneratedHashTitle(nil, "DOCS", title)
	}

	for name, tweak := range map[string]func(*Config, string){
		"flat":      func(*Config, string) {},
		"hierarchy": func(c *Config, dir string) { c.ParentsFromPath = true; c.ParentsFromPathRoot = dir },
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeFile(t, dir, "parent.md", parentDoc("Parent"))
			writeFile(t, dir, "a-child.md", childDoc(hashed("Parent")))
			config := trackingConfig(server, dir+"/*.md")
			config.TitleAppendGeneratedHash = true
			tweak(&config, dir)
			require.NoError(t, Run(config))

			parentID := pageIDTitled(t, server, hashed("Parent"))
			childID := pageIDTitled(t, server, metadata.GeneratedHashTitle([]string{hashed("Parent")}, "DOCS", "Child"))
			require.NotEmpty(t, parentID)
			require.NotEmpty(t, childID)

			writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
			writeFile(t, dir, "a-child.md", childDoc(hashed("Parent Renamed")))
			require.NoError(t, Run(config))

			assert.Equal(t, parentID, pageIDTitled(t, server, hashed("Parent Renamed")), "renamed in place")
			assert.Empty(t, pageIDTitled(t, server, hashed("Parent")))
			assert.Equal(t, parentID, server.Page(childID).ParentID, "child stays under the original page")
		})
	}
}

// TestRetitledParentByRenamingItsFile: the parent takes its title from its
// filename, so renaming the file is what retitles it. Its path is not recorded
// yet, and the page it published is found by its content, as the parent's own
// turn will find it.
func TestRetitledParentByRenamingItsFile(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	parent := "<!-- Space: DOCS -->\n\nParent body.\n"
	writeFile(t, dir, "parent-doc.md", parent)
	writeFile(t, dir, "a-child.md", childDoc("Parent Doc"))
	config := trackingConfig(server, dir+"/*.md")
	config.TitleFromFilename = true
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent Doc")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)
	require.NotEmpty(t, childID)

	require.NoError(t, os.Rename(filepath.Join(dir, "parent-doc.md"), filepath.Join(dir, "renamed-doc.md")))
	writeFile(t, dir, "a-child.md", childDoc("Renamed Doc"))
	require.NoError(t, Run(config))

	assert.Equal(t, parentID, pageIDTitled(t, server, "Renamed Doc"), "renamed in place")
	assert.Empty(t, pageIDTitled(t, server, "Parent Doc"))
	assert.Equal(t, parentID, server.Page(childID).ParentID, "child stays under the original page")
}

// TestRetitledBlogPostIsNotAParent: a blog post retitled to the title a child
// now names is not that child's parent. The parent is a page that does not
// exist yet, and is created.
func TestRetitledBlogPostIsNotAParent(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	post := func(title string) string {
		return "<!-- Space: DOCS -->\n<!-- Type: blogpost -->\n<!-- Title: " + title + " -->\n\nNews.\n"
	}
	writeFile(t, dir, "news.md", post("News"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, childID)

	writeFile(t, dir, "news.md", post("Section"))
	writeFile(t, dir, "a-child.md", childDoc("Section"))
	require.NoError(t, Run(config))

	sectionID := pageIDTitled(t, server, "Section")
	require.NotEmpty(t, sectionID, "the parent the child names is created")
	assert.Equal(t, sectionID, server.Page(childID).ParentID)
	assert.Empty(t, pageIDTitled(t, server, "News"), "no page under the blog post's old title")
}

// TestRetitledDirectoryDocument: with a path hierarchy, a directory's README
// titles the directory, and the documents beside it name that title as their
// parent. API.md sorts ahead of README.md, so it looks for the new title
// before the README has retitled its page.
func TestRetitledDirectoryDocument(t *testing.T) {
	readme := func(title string) string {
		return "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n\nGuide.\n"
	}
	api := "<!-- Space: DOCS -->\n<!-- Title: API -->\n\nAPI.\n"

	for name, hash := range map[string]bool{"plain": false, "generated hash": true} {
		t.Run(name, func(t *testing.T) {
			guideTitle := func(title string) string {
				if hash {
					return metadata.GeneratedHashTitle(nil, "DOCS", title)
				}
				return title
			}
			apiTitle := "API"
			if hash {
				apiTitle = metadata.GeneratedHashTitle([]string{guideTitle("Guide")}, "DOCS", "API")
			}

			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeAt(t, dir, "guide/README.md", readme("Guide"))
			writeAt(t, dir, "guide/API.md", api)
			config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
			config.ParentsFromPath = true
			config.ParentsFromPathRoot = dir
			config.TitleAppendGeneratedHash = hash
			require.NoError(t, Run(config))

			guideID := pageIDTitled(t, server, guideTitle("Guide"))
			apiID := pageIDTitled(t, server, apiTitle)
			require.NotEmpty(t, guideID)
			require.NotEmpty(t, apiID)
			require.Equal(t, guideID, server.Page(apiID).ParentID)

			writeAt(t, dir, "guide/README.md", readme("Guide Renamed"))
			require.NoError(t, Run(config))

			assert.Equal(t, guideID, pageIDTitled(t, server, guideTitle("Guide Renamed")), "renamed in place")
			assert.Empty(t, pageIDTitled(t, server, guideTitle("Guide")))
			assert.Equal(t, guideID, server.Page(apiID).ParentID, "API stays under the original page")
		})
	}
}

// TestRetitledChainOfThree: every ancestor is retitled with the document below
// it, and the deepest document sorts first.
func TestRetitledChainOfThree(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	titled := func(title string, parents ...string) string {
		doc := "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n"
		for _, parent := range parents {
			doc += "<!-- Parent: " + parent + " -->\n"
		}
		return doc + "\nBody.\n"
	}
	write := func(grand, parent string) {
		writeFile(t, dir, "a-leaf.md", titled("Leaf", grand, parent))
		writeFile(t, dir, "b-parent.md", titled(parent, grand))
		writeFile(t, dir, "c-grand.md", titled(grand))
	}
	// Published ancestors first, so the first run does not meet the ordering itself.
	writeFile(t, dir, "b-parent.md", titled("Parent", "Grand"))
	writeFile(t, dir, "c-grand.md", titled("Grand"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))
	write("Grand", "Parent")
	require.NoError(t, Run(config))

	grandID := pageIDTitled(t, server, "Grand")
	parentID := pageIDTitled(t, server, "Parent")
	leafID := pageIDTitled(t, server, "Leaf")
	require.NotEmpty(t, leafID)

	write("Grand Renamed", "Parent Renamed")
	require.NoError(t, Run(config))

	assert.Equal(t, grandID, pageIDTitled(t, server, "Grand Renamed"))
	assert.Equal(t, parentID, pageIDTitled(t, server, "Parent Renamed"))
	assert.Equal(t, parentID, server.Page(leafID).ParentID)
	assert.Equal(t, grandID, server.Page(parentID).ParentID)
}

// TestRetitledParentWithFrontMatter: the same retitle, with titles and parents
// given as front matter.
func TestRetitledParentWithFrontMatter(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	parent := func(title string) string {
		return "---\nspace: DOCS\ntitle: " + title + "\n---\n\nBody.\n"
	}
	child := func(parentTitle string) string {
		return "---\nspace: DOCS\ntitle: Child\nparents:\n  - " + parentTitle + "\n---\n\nBody.\n"
	}
	writeFile(t, dir, "parent.md", parent("Parent"))
	writeFile(t, dir, "a-child.md", child("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	config.Features = append(config.Features, "frontmatter")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, childID)

	writeFile(t, dir, "parent.md", parent("Parent Renamed"))
	writeFile(t, dir, "a-child.md", child("Parent Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, parentID, pageIDTitled(t, server, "Parent Renamed"))
	assert.Equal(t, parentID, server.Page(childID).ParentID)
}

// TestMovedDocumentKeepsItsPage: a document moved to another directory with its
// title unchanged is found by that title, and the page follows it. The path it
// moved from must not then read as an orphan - its page is the one just
// published, and --on-orphan delete would trash it in the same run.
func TestMovedDocumentKeepsItsPage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o755))
	writeFile(t, dir, "a/doc.md", markdownWithTitle("Moved"))
	writeFile(t, dir, "keep.md", markdownWithTitle("Keep"))

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.OnOrphan = "delete"
	require.NoError(t, Run(config))

	before, err := api.FindPage("DOCS", "Moved", "page")
	require.NoError(t, err)
	require.NotNil(t, before)

	require.NoError(t, os.Rename(filepath.Join(dir, "a", "doc.md"), filepath.Join(dir, "b", "doc.md")))
	require.NoError(t, Run(config))

	page := server.Page(before.ID)
	require.NotNil(t, page)
	assert.False(t, page.Trashed, "the moved document's page must not be trashed")

	// Deleting the moved document afterwards still removes its page.
	require.NoError(t, os.Remove(filepath.Join(dir, "b", "doc.md")))
	require.NoError(t, Run(config))
	assert.True(t, server.Page(before.ID).Trashed)
}

// TestNoOverwriteKeepsAnEditOnTheRunADocumentMoves: the run on which a
// document moves is the one run where its new path has no entry, so the
// version --no-overwrite compares against is in the entry of the path it moved
// from. Asking the new path alone found no baseline and overwrote an edit made
// in Confluence -- and the run after, had the move not carried the version
// across, would have done the same.
func TestNoOverwriteKeepsAnEditOnTheRunADocumentMoves(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Moved"))

	config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
	config.NoOverwrite = true
	require.NoError(t, Run(config))

	published, err := api.FindPage("DOCS", "Moved", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	server.EditPage(published.ID, "<p>Written by a person.</p>")
	edited := server.Page(published.ID)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(dir, "doc.md"), filepath.Join(dir, "sub", "doc.md")))

	require.NoError(t, Run(config))
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(published.ID).Body,
		"the run that moves the document must not overwrite the edit")
	assert.Equal(t, edited.Version, server.Page(published.ID).Version)

	require.NoError(t, Run(config))
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(published.ID).Body,
		"nor may the run after it")
}

// publishOnce runs one file through ProcessFile against the fake, with a
// fresh client so nothing is answered from an earlier lookup's cache.
func publishOnce(t *testing.T, server *confluencetest.Server, file string) {
	t.Helper()

	api := confluence.NewAPI(server.URL, "user", "token", false)
	_, err := ProcessFile(file, api, Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Output:   io.Discard,
	})
	require.NoError(t, err)
}

// TestAPageOutsideItsParentsIsMovedIntoItsFolder: a page that already exists,
// found by title somewhere other than under its declared Parent, is the page
// the document means -- a title is unique within a space. Treating it as a
// stranger had mark create a second page with the same title, which
// Confluence refuses, instead of moving the one that is there.
func TestAPageOutsideItsParentsIsMovedIntoItsFolder(t *testing.T) {
	server, _ := docsSpace(t)
	home := server.Page("1002")
	require.NotNil(t, home)
	require.Equal(t, "Home", home.Title)
	doc := server.AddPage("DOCS", "Doc", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownInFolder("Manuals", "Doc"))

	publishOnce(t, server, file)

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Equal(t, 1, countPagesTitled(t, server, "Doc"))
	assert.Equal(t, folders[0].ID, server.Page(doc.ID).ParentID,
		"the existing page should have been moved into its folder")
	assert.Contains(t, server.Page(doc.ID).Body, "Body.", "and published in place")
}

// reparentScenario builds root > first > Old > Doc beside root > first > New
// and publishes Doc with its headers naming first > New. With first set to the
// space home page, root is empty and first is Home itself.
func reparentScenario(t *testing.T, first string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	anchor := home
	if first != "Home" {
		anchor = server.AddPage("DOCS", first, "page", home.ID)
	}
	old := server.AddPage("DOCS", "Old", "page", anchor.ID)
	want := server.AddPage("DOCS", "New", "page", anchor.ID)
	doc := server.AddPage("DOCS", "Doc", "page", old.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: "+first+" -->\n<!-- Parent: New -->\n"+
			"<!-- Title: Doc -->\n\nbody\n")

	publishOnce(t, server, file)

	assert.Equal(t, want.ID, server.Page(doc.ID).ParentID, "the page should have moved under New")
}

// TestAPageIsMovedWhenItsFirstParentIsTheHomePage: a page at Home > Old whose
// headers now say Home > New is moved under New, as it would be under any other
// first parent. With the home page first, the page's own title was left out of
// the ancestry checked, and the check that followed accepted any ancestor
// matching any declared parent -- and Home always matches.
func TestAPageIsMovedWhenItsFirstParentIsTheHomePage(t *testing.T) {
	reparentScenario(t, "Home")
}

// TestAPageIsMovedWhenItsFirstParentIsAnOrdinaryPage is the same move under a
// first parent that is not the home page, which always worked.
func TestAPageIsMovedWhenItsFirstParentIsAnOrdinaryPage(t *testing.T) {
	reparentScenario(t, "Eng")
}

// TestATrackedPageIsMovedWhenItsFirstParentIsTheHomePage is the same move
// arriving through the manifest: retitled and reparented in one edit, so the
// title lookup misses and only the check after it can see the page is in the
// wrong place. That check was satisfied by any declared parent, and Home is
// among the ancestors of a page under Home > Old.
func TestATrackedPageIsMovedWhenItsFirstParentIsTheHomePage(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Old", "page", home.ID)
	want := server.AddPage("DOCS", "New", "page", home.ID)

	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Home -->\n"
	file := writeFile(t, dir, "doc.md", header+"<!-- Parent: Old -->\n<!-- Title: Doc -->\n\nbody\n")
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	published, err := confluence.NewAPI(server.URL, "user", "token", false).FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	writeFile(t, dir, "doc.md", header+"<!-- Parent: New -->\n<!-- Title: Doc Renamed -->\n\nbody\n")
	require.NoError(t, Run(config))

	after := server.Page(published.ID)
	require.NotNil(t, after)
	assert.Equal(t, "Doc Renamed", after.Title)
	assert.Equal(t, want.ID, after.ParentID, "the page should have moved under New")
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

// TestRenamedHandMadeParentIsFollowedNotRecreated is the case the title index
// could never answer. Following a stale parent used to mean asking which page
// mark had published under that title, and a parent nobody publishes -- made by
// hand, or created empty by mark to hold a hierarchy -- has never been in that
// index. The chain each document declares is recorded when it resolves, so the
// position is what is followed rather than the title.
func TestRenamedHandMadeParentIsFollowedNotRecreated(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	guide := handMadeParent(t, server, "Guide")

	file := writeFile(t, dir, "child.md", markdownUnder("Guide", "Child"))
	require.NoError(t, Run(trackingConfig(server, file)))

	// Somebody renames it in the UI. The document still says "Guide".
	server.RenamePage(guide.ID, "Guide Renamed")
	require.NoError(t, Run(trackingConfig(server, file)))

	assert.Equal(t, 0, countPagesTitled(t, server, "Guide"),
		"no empty page should have been created under the old title")

	child, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.Equal(t, guide.ID, server.Page(child.ID).ParentID,
		"the child should still sit under the page it always did")
}

// TestRenamedHandMadeParentDuplicatesWithoutTracking is the control: without
// the flag the old behaviour stands, which is what makes the test above mean
// anything.
func TestRenamedHandMadeParentDuplicatesWithoutTracking(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	guide := handMadeParent(t, server, "Guide")

	config := trackingConfig(server, writeFile(t, dir, "child.md", markdownUnder("Guide", "Child")))
	config.TrackPages = false
	require.NoError(t, Run(config))

	server.RenamePage(guide.ID, "Guide Renamed")
	require.NoError(t, Run(config))

	assert.Equal(t, 1, countPagesTitled(t, server, "Guide"),
		"without tracking an empty page appears under the old title")

	child, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.NotEqual(t, guide.ID, server.Page(child.ID).ParentID,
		"and the child is moved beneath it")
}

// TestParentCreatedByMarkIsFollowedAfterRename covers the other half of the
// recording: a parent mark created itself to hold the hierarchy is remembered
// at the moment it is created, not only when it is found again.
func TestParentCreatedByMarkIsFollowedAfterRename(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "child.md", markdownUnder("Brand New Parent", "Child"))
	require.NoError(t, Run(trackingConfig(server, file)))

	created, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Brand New Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, created)

	server.RenamePage(created.ID, "Brand New Parent Renamed")
	require.NoError(t, Run(trackingConfig(server, file)))

	assert.Equal(t, 0, countPagesTitled(t, server, "Brand New Parent"),
		"the parent mark created should have been followed, not made again")
}

// TestRenamedParentChainIsFollowedAtEveryLevel: the keys name the chain as the
// document declares it. Rewriting one level and then keying the next off the
// rewritten title would look up a chain that was never recorded, so a run where
// two levels were renamed at once would repair the first and recreate the
// second.
func TestRenamedParentChainIsFollowedAtEveryLevel(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	top, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	guide := handMadeParent(t, server, "Guide")

	file := writeFile(t, dir, "child.md", markdownUnder("Guide", "Child"))
	require.NoError(t, Run(trackingConfig(server, file)))

	server.RenamePage(top.ID, "Parent Renamed")
	server.RenamePage(guide.ID, "Guide Renamed")
	require.NoError(t, Run(trackingConfig(server, file)))

	assert.Equal(t, 0, countPagesTitled(t, server, "Parent"),
		"the top of the chain should have been followed")
	assert.Equal(t, 0, countPagesTitled(t, server, "Guide"),
		"and so should the level below it, whose key is unaffected by the rewrite above")

	child, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.Equal(t, guide.ID, server.Page(child.ID).ParentID,
		"the child should not have moved")
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
