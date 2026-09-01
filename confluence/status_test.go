package confluence_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindPageCarriesStatus pins that live content can be told from the rest at
// all. PageInfo had no status field, so nothing downstream could distinguish a
// current page from an archived one even when the response said so.
func TestFindPageCarriesStatus(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Live", "page", "")

	page, err := api.FindPage("DOCS", "Live", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "current", page.Status)
}

// TestFindPageDoesNotSeeAnArchivedPage is the fact the whole diagnosis rests
// on: v1 filters a lookup to current, so the page is gone from mark's view
// while Confluence still knows about it.
func TestFindPageDoesNotSeeAnArchivedPage(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Retired", "page", "")
	require.NoError(t, api.ArchivePage(page.ID))

	found, err := api.FindPage("DOCS", "Retired", "page")
	require.NoError(t, err)
	assert.Nil(t, found, "an archived page is not current content")
}

// TestCreatePageNamesTheArchivedPageHoldingTheTitle pins the class of bug where
// a failure has exactly one plausible cause and the error names none of them.
//
// --on-orphan archive archives a page; the source file comes back; FindPage
// returns nil because v1 hides archived content; CreatePage fails with a bare
// 400 Bad Request. There was no way forward from that message short of
// guessing, and the guess is un-archiving by hand.
//
// The diagnosis is a message and nothing else. Whether an archived page truly
// blocks reuse of its title is not verifiable without a live instance, and
// restoring somebody's page on a hunch is not mark's decision to make.
func TestCreatePageNamesTheArchivedPageHoldingTheTitle(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Retired", "page", "")
	require.NoError(t, api.ArchivePage(page.ID))

	_, err := api.CreatePage("DOCS", "page", nil, "Retired", "<p/>")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "archived")
	assert.Contains(t, err.Error(), "Retired", "the title has to be in the message")
	assert.Contains(t, err.Error(), "DOCS", "and so does the space")
	assert.Contains(t, err.Error(), page.ID, "and the id of the page to go and look at")
}

// TestCreatePageNamesATrashedPageHoldingTheTitle covers the other invisible
// state. --on-orphan trash is the default, so this is the commoner of the two.
func TestCreatePageNamesATrashedPageHoldingTheTitle(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Deleted", "page", "")
	require.NoError(t, api.DeletePage(page.ID))

	_, err := api.CreatePage("DOCS", "page", nil, "Deleted", "<p/>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trashed")
	assert.Contains(t, err.Error(), page.ID)
}

// TestCreatePageLeavesUnrelatedFailuresAlone: the diagnosis must not attach
// itself to a create that failed for some other reason, or every error grows a
// paragraph that does not apply to it.
func TestCreatePageLeavesUnrelatedFailuresAlone(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.AddPage("DOCS", "Taken", "page", "")

	_, err := api.CreatePage("DOCS", "page", nil, "Taken", "<p/>")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "archived")
	assert.NotContains(t, err.Error(), "trashed")
}
