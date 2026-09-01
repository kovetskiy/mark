package mark

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderOrderedDocument is a document that declares both a folder and a
// position among its siblings -- the combination that used to get neither.
func folderOrderedDocument(title string, order string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Folder: Manuals -->
<!-- Title: ` + title + ` -->
<!-- Order: ` + order + ` -->

# ` + title + `

Some content.
`
}

// titlesInOrder names the pages Confluence lists under a parent, in the order
// the tree shows them.
func titlesInOrder(server *confluencetest.Server, parentID string) []string {
	var titles []string
	for _, id := range server.ChildOrder(parentID) {
		if page := server.Page(id); page != nil {
			titles = append(titles, page.Title)
		}
	}

	return titles
}

// TestOrderIsAppliedUnderAFolder pins the class of bug where a placement is
// dropped for want of a parent id. A folder is not a page, so it never appears
// among a page's expanded ancestors -- every folder-parented page therefore
// reported no parent, and OrderChildren discarded its position without a word.
func TestOrderIsAppliedUnderAFolder(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", folderOrderedDocument("Second", "2"))
	writeFile(t, dir, "b.md", folderOrderedDocument("First", "1"))

	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

	folders := server.Folders()
	require.Len(t, folders, 1, "the declared folder should have been created once")

	assert.Equal(t, []string{"First", "Second"}, titlesInOrder(server, folders[0].ID),
		"a page under a folder must still be placed where its Order header asks")
}

// TestOrderIsAppliedUnderAPage guards the ordinary path, which resolves the
// parent from the page's own ancestors, against the folder fallback added
// beside it.
func TestOrderIsAppliedUnderAPage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	document := func(title, order string) string {
		return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: ` + title + ` -->
<!-- Order: ` + order + ` -->

# ` + title + `

Some content.
`
	}

	writeFile(t, dir, "a.md", document("Second", "2"))
	writeFile(t, dir, "b.md", document("First", "1"))

	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))

	parent, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, parent)

	assert.Equal(t, []string{"First", "Second"}, titlesInOrder(server, parent.ID))
}
