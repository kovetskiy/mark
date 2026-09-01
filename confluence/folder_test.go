package confluence_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
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
