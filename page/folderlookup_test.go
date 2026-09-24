package page_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFolderLookupIgnoresASameTitledFolderFurtherDown: the folder beneath a
// parent used to be found by CQL's ancestor=, which matches at any depth, with
// limit=1. A folder of the same title deeper in the tree could come back
// first, and was rightly refused for having the wrong parent -- after which
// the create collided with the real one, and the lookup that follows a failed
// create found the same wrong folder again. The run failed on a hierarchy that
// was already exactly as declared.
func TestFolderLookupIgnoresASameTitledFolderFurtherDown(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")

	// Added first, so that anything answering in insertion order reaches the
	// nested ones before the ones that are really wanted.
	other := server.AddFolder("DOCS", "Other", root.ID, "page")
	server.AddFolder("DOCS", "Manuals", other.ID, "folder")
	manuals := server.AddFolder("DOCS", "Manuals", root.ID, "page")
	deeper := server.AddFolder("DOCS", "Other", manuals.ID, "folder")
	server.AddFolder("DOCS", "Guides", deeper.ID, "folder")
	guides := server.AddFolder("DOCS", "Guides", manuals.ID, "folder")

	before := len(server.Folders())

	parent, err := page.EnsureFolderAncestry(
		false, api, "DOCS", []string{"Manuals", "Guides"}, &root.ID, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, guides.ID, parent.ID)
	assert.Len(t, server.Folders(), before, "nothing should have been created")
}

// TestRootFolderLookupIgnoresASameTitledNestedFolder is the space-root side of
// the same thing. There is no listing of a space's root folders, so that
// lookup still searches -- and with limit=1 a nested folder of the same title
// hid the one at the root, which a run under a new --parents anchor exists to
// find and move.
func TestRootFolderLookupIgnoresASameTitledNestedFolder(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	elsewhere := server.AddPage("DOCS", "Elsewhere", "page", "")

	server.AddFolder("DOCS", "Manuals", elsewhere.ID, "page")
	atRoot := server.AddFolder("DOCS", "Manuals", "", "")

	// A dry run, because the fake does not move folders; it reports the move
	// and hands back the folder it would have moved.
	parent, err := page.EnsureFolderAncestry(true, api, "DOCS", []string{"Manuals"}, &anchor.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, atRoot.ID, parent.ID)
}
