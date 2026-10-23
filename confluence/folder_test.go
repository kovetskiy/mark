package confluence_test

import (
	"fmt"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateFolderUnderAVanishedParent: GetFolderByID answers (nil, nil) for a
// folder that is not there, not an error, and CreateFolder checked only the
// error before reading the parent's space id -- so it panicked rather than
// failed.
//
// The id reaching it comes from the folder cache or from the manifest, meaning
// it names a folder that existed when it was recorded. A folder deleted in
// Confluence between two runs is exactly the case folder tracking exists to
// survive, so this crashed on the recovery path.
func TestCreateFolderUnderAVanishedParent(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	space := server.AddSpace("DOCS")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	gone := "no-such-folder"
	folder, err := api.CreateFolder(space.ID, "Guides", &gone, "folder")

	require.NoError(t, err, "a missing parent must not fail the call")
	require.NotNil(t, folder)
	assert.Equal(t, "Guides", folder.Title)
}

// TestFindChildFolderReadsEveryPage: the folder can be past the first page of
// a parent's children, behind any number of pages.
func TestFindChildFolderReadsEveryPage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	for i := range 150 {
		server.AddPage("DOCS", fmt.Sprintf("Child %d", i), "page", parent.ID)
	}
	want := server.AddFolder("DOCS", "Guides", parent.ID, "page")

	folder, err := api.FindChildFolder(parent.ID, "page", "Guides")
	require.NoError(t, err)
	require.NotNil(t, folder)
	assert.Equal(t, want.ID, folder.ID)

	missing, err := api.FindChildFolder(parent.ID, "page", "Nothing")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// TestFindChildFolderUnderAFolder reads the folder listing, not the page one,
// and only the folder's own children.
func TestFindChildFolderUnderAFolder(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	top := server.AddFolder("DOCS", "Top", "", "")
	middle := server.AddFolder("DOCS", "Middle", top.ID, "folder")
	server.AddFolder("DOCS", "Guides", middle.ID, "folder")

	folder, err := api.FindChildFolder(top.ID, "folder", "Guides")
	require.NoError(t, err)
	assert.Nil(t, folder, "a grandchild is not a child")

	want := server.AddFolder("DOCS", "guides", top.ID, "folder")
	folder, err = api.FindChildFolder(top.ID, "folder", "Guides")
	require.NoError(t, err)
	require.NotNil(t, folder, "a title differing only in case still matches")
	assert.Equal(t, want.ID, folder.ID)
}

// TestFindRootFolderFollowsTheSearch: every same-titled folder nested in the
// space can come before the one at the root, across more than one page of
// search results.
func TestFindRootFolderFollowsTheSearch(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	for range 30 {
		server.AddFolder("DOCS", "Guides", parent.ID, "page")
	}
	want := server.AddFolder("DOCS", "Guides", "", "")

	folder, err := api.FindRootFolder("DOCS", "Guides")
	require.NoError(t, err)
	require.NotNil(t, folder)
	assert.Equal(t, want.ID, folder.ID)

	missing, err := api.FindRootFolder("DOCS", "Nothing")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
