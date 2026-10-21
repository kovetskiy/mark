package mark

import (
	"io"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
