package page_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	return confluence.NewAPI(server.URL, "user", "token", false), server
}

// titles collects the stored titles of every page in a space, for asserting on
// what a run created.
func titles(t *testing.T, server *confluencetest.Server, api *confluence.API, space string, want ...string) {
	t.Helper()
	for _, title := range want {
		found, err := api.FindPage(space, title, "page")
		require.NoError(t, err)
		assert.NotNil(t, found, "expected page %q to exist", title)
	}
}

func TestEnsureAncestryAllParentsExist(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	team := server.AddPage("DOCS", "Team", "page", root.ID)
	server.AddPage("DOCS", "Guides", "page", team.ID)

	parent, err := page.EnsureAncestry(false, api, "DOCS", []string{"Root", "Team", "Guides"}, nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, "Guides", parent.Title)

	assert.Equal(t, 0, server.CountRequests("POST", "/rest/api/content"),
		"nothing should be created when the whole chain already exists")
}

func TestEnsureAncestryCreatesMissingParents(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Root", "page", "")

	parent, err := page.EnsureAncestry(false, api, "DOCS", []string{"Root", "Team", "Guides"}, nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, "Guides", parent.Title)

	titles(t, server, api, "DOCS", "Team", "Guides")

	// Team under Root, Guides under Team.
	team, err := api.FindPage("DOCS", "Team", "page")
	require.NoError(t, err)
	guides, err := api.FindPage("DOCS", "Guides", "page")
	require.NoError(t, err)

	assert.Equal(t, "Root", team.Ancestors[len(team.Ancestors)-1].Title)
	assert.Equal(t, "Team", guides.Ancestors[len(guides.Ancestors)-1].Title)
}

// TestEnsureAncestryDryRunCreatesNothing is the guard for the class of bug in
// issue #572: a dry run must not write.
func TestEnsureAncestryDryRunCreatesNothing(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Root", "page", "")

	_, err := page.EnsureAncestry(true, api, "DOCS", []string{"Root", "Team", "Guides"}, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, server.CountRequests("POST", "/rest/api/content"),
		"dry run must not create pages")
	assert.Equal(t, 0, server.CountRequests("PUT", "/rest/api/content"),
		"dry run must not update pages")
}

// TestEnsureAncestryFallsBackToRootPage covers the branch where none of the
// requested ancestors exist yet, so the space's first page anchors the chain.
func TestEnsureAncestryFallsBackToRootPage(t *testing.T) {
	api, server := newAPI(t)
	existing := server.AddPage("DOCS", "Existing", "page", "")

	parent, err := page.EnsureAncestry(false, api, "DOCS", []string{"Brand New"}, nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, "Brand New", parent.Title)

	created, err := api.FindPage("DOCS", "Brand New", "page")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotEmpty(t, created.Ancestors)
	assert.Equal(t, existing.ID, created.Ancestors[len(created.Ancestors)-1].ID)
}

func TestValidateAncestryMatchingChain(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	team := server.AddPage("DOCS", "Team", "page", root.ID)
	server.AddPage("DOCS", "Guides", "page", team.ID)

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Root", "Team", "Guides"})
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Guides", found.Title)
}

func TestValidateAncestryMissingPageReturnsNil(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Root", "page", "")

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Root", "Nope"})
	require.NoError(t, err)
	assert.Nil(t, found)
}

// TestValidateAncestryWrongParentIsRejected is the check that stops a page
// being adopted by the wrong branch of the tree -- the failure mode behind
// issues #450 and #430.
func TestValidateAncestryWrongParentIsRejected(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	other := server.AddPage("DOCS", "Other", "page", root.ID)
	server.AddPage("DOCS", "Guides", "page", other.ID)

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Root", "Team", "Guides"})
	require.Error(t, err)
	// Guides sits two levels deep but three were expected, so the depth check
	// rejects it before the per-parent comparison runs.
	assert.ErrorIs(t, err, page.ErrAncestryMismatch)
	assert.Contains(t, err.Error(), "actual=[Root > Other]")
	assert.NotNil(t, found,
		"the page is returned alongside the error, so the caller can move it")
}

// TestValidateAncestryWrongParentAtCorrectDepth reaches the per-parent
// comparison, which the depth check above short-circuits: the chain is as deep
// as expected but names a different intermediate page.
func TestValidateAncestryWrongParentAtCorrectDepth(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	other := server.AddPage("DOCS", "Other", "page", root.ID)
	extra := server.AddPage("DOCS", "Extra", "page", other.ID)
	server.AddPage("DOCS", "Guides", "page", extra.ID)

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Root", "Team", "Guides"})
	require.Error(t, err)
	assert.ErrorIs(t, err, page.ErrAncestryMismatch)
	assert.Contains(t, err.Error(), `expected parent "Team"`)
	assert.NotNil(t, found)
}

// TestValidateAncestryHomepageWithoutAncestors covers the special case where a
// page legitimately has no parents because it is the space homepage.
func TestValidateAncestryHomepageWithoutAncestors(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Home"})
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Home", found.Title)
}

// TestValidateAncestryRootLevelPageIsMisplaced is the same shape as the
// homepage case but for a page that is not the homepage. A document declaring
// no parents is placed under the space root page, so a page sitting at the root
// instead disagrees with that -- a misplacement, reported as one so the caller
// can move it.
//
// It used to be a flat refusal, which made such a page unpublishable by mark at
// all: declaring the parent it ought to have is exactly what produces this
// state, so no header could get past it.
func TestValidateAncestryRootLevelPageIsMisplaced(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	orphan := server.AddPage("DOCS", "Orphan", "page", "")

	found, err := page.ValidateAncestry(api, "DOCS", []string{"Orphan"})
	require.Error(t, err)
	assert.ErrorIs(t, err, page.ErrAncestryMismatch)
	require.NotNil(t, found, "the page comes back with the error, so it can be moved")
	assert.Equal(t, orphan.ID, found.ID)
}
