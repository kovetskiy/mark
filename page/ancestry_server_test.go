package page_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/page"
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
// requested ancestors exist yet in a space that has no home page, so the
// space's first page anchors the chain.
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

// TestEnsureAncestryCreatesMissingParentsUnderTheHomePage is the case the root
// lookup used to get wrong: a space with a second page at its root, listed
// ahead of the home page. Missing parents belong under the home page -- the
// page ValidateAncestry measures against -- not under whatever the space
// listing returns first.
func TestEnsureAncestryCreatesMissingParentsUnderTheHomePage(t *testing.T) {
	api, server := newAPI(t)
	other := server.AddPage("DOCS", "Archive", "page", "")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	first, err := api.FindPage("DOCS", "", "page")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, other.ID, first.ID,
		"the test needs the other root page listed first to mean anything")

	parent, err := page.EnsureAncestry(false, api, "DOCS", []string{"Team", "Guides"}, nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, "Guides", parent.Title)

	assert.Equal(t, []string{"Home"}, ancestorTitles(t, api, "Team"))
	assert.Equal(t, []string{"Home", "Team"}, ancestorTitles(t, api, "Guides"))

	_, err = page.ValidateAncestry(api, "DOCS", []string{"Team", "Guides"})
	assert.NoError(t, err, "what was just created must validate against the home page")
}

// ancestorTitles reports the titles a page sits under, outermost first, as the
// server stores them.
func ancestorTitles(t *testing.T, api *confluence.API, title string) []string {
	t.Helper()

	found, err := api.FindPage("DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, found, "page %q should exist", title)

	stored, err := api.GetPageByIDExpanded(found.ID, "ancestors")
	require.NoError(t, err)

	var titles []string
	for _, ancestor := range stored.Ancestors {
		titles = append(titles, ancestor.Title)
	}

	return titles
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

// Confluence compares titles without regard to case, so a chain that differs
// from the stored one only in case names the same pages -- the page itself
// included, which the lookup finds by its title.
func TestValidateAncestryIgnoresCase(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	team := server.AddPage("DOCS", "Team", "page", root.ID)
	guides := server.AddPage("DOCS", "Guides", "page", team.ID)

	found, err := page.ValidateAncestry(api, "DOCS", []string{"root", "TEAM", "guides"})
	require.NoError(t, err, "a parent differing only in case is the same parent")
	require.NotNil(t, found)
	assert.Equal(t, guides.ID, found.ID)

	_, err = page.ValidateAncestry(api, "DOCS", []string{"Elsewhere", "Team", "Guides"})
	assert.ErrorIs(t, err, page.ErrAncestryMismatch, "a different parent is still a mismatch")
}

// TestDryRunDoesNotMoveAFolder covers the one write a dry run was still making.
//
// A folder left at the space root by an earlier sync is moved under the page
// --parents names. That happens while the ancestry is being worked out, which
// is long before the guard deciding whether this run writes anything -- so the
// mode that promises to write nothing reparented a folder, and every page
// inside it went with it.
func TestDryRunDoesNotMoveAFolder(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "Anchor", "page", "")

	// At the space root, where an earlier sync left it: no page or folder
	// parent, which is what makes it a candidate for the move.
	server.AddFolder("DOCS", "Manuals", "", "")

	var moved atomic.Bool

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			moved.Store(true)
		}

		return 0, "", false
	})

	_, err := page.EnsureFolderAncestry(true, api, "DOCS", []string{"Manuals"}, &anchor.ID, nil)
	require.NoError(t, err)

	assert.False(t, moved.Load(), "a dry run must not move anything")

	folders := server.Folders()
	require.Len(t, folders, 1)
	assert.Empty(t, folders[0].ParentID, "the folder must still be where it was")
}

// TestARealRunStillAsksToMoveTheFolder is the control: the move is wanted, and
// only the mode that writes nothing should be without it.
//
// The request is what is asserted rather than the folder's new parent, because
// the fake Confluence moves pages and not folders -- so the move is observed
// going out, and whatever it answers is beside the point here.
func TestARealRunStillAsksToMoveTheFolder(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "Anchor", "page", "")
	server.AddFolder("DOCS", "Manuals", "", "")

	var moved atomic.Bool

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			moved.Store(true)
		}

		return 0, "", false
	})

	_, _ = page.EnsureFolderAncestry(false, api, "DOCS", []string{"Manuals"}, &anchor.ID, nil)

	assert.True(t, moved.Load(), "a real run asks to move the folder")
}

// TestDryRunMixedAncestryIgnoresCaseOfAnchorTitle: a MARK_PARENTS chain that
// differs from the existing anchor page only in case names that page, so a dry
// run finds the folders beneath it rather than standing in for ones a real run
// would create.
func TestDryRunMixedAncestryIgnoresCaseOfAnchorTitle(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	anchor := server.AddPage("DOCS", "My Parent", "page", "")
	folder := server.AddFolder("DOCS", "Manuals", anchor.ID, "page")

	parent, err := page.EnsureMixedAncestry(true, api, nil, "DOCS", []string{"Manuals"}, []string{"my parent"})
	require.NoError(t, err)
	require.NotNil(t, parent)

	assert.Equal(t, folder.ID, parent.ID, "the existing folder under the anchor is the parent")
}

// TestFolderCreateConflictDoesNotDependOnWording pins the class of bug where an
// error is identified by the text Confluence happened to put in it. A folder
// another file created moments earlier makes the create fail, and recognising
// that only by the English phrase "folder exists with the same title" turned a
// recoverable collision into a hard failure the moment the wording changed or
// the instance answered in another language.
func TestFolderCreateConflictDoesNotDependOnWording(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")

	// The folder another file in this run already created. It is hidden from
	// the first listing so that this run believes it has to create it.
	server.AddFolder("DOCS", "Manuals", root.ID, "page")

	searched := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if !searched && strings.HasSuffix(r.URL.Path, "/direct-children") {
			searched = true

			return http.StatusOK, `{"results":[]}`, true
		}

		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v2/folders") {
			// Confluence's own refusal, in a wording no caller may rely on.
			return http.StatusBadRequest,
				`{"errors":[{"title":"Es existiert bereits ein Ordner mit diesem Titel"}]}`,
				true
		}

		return 0, "", false
	})

	parent, err := page.EnsureFolderAncestry(false, api, "DOCS", []string{"Manuals"}, &root.ID, nil)
	require.NoError(t, err, "a folder that already exists is not a failure")
	require.NotNil(t, parent)
	assert.Equal(t, "Manuals", parent.Title)

	assert.Len(t, server.Folders(), 1, "no second folder should have been created")
}

// TestFolderCreateFailureIsStillReported guards the other side: falling back to
// a lookup must not turn a genuine refusal into silence. Nothing carries the
// title, so there is nothing to find and the create's own error stands.
func TestFolderCreateFailureIsStillReported(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v2/folders") {
			return http.StatusForbidden, `{"errors":[{"title":"not permitted"}]}`, true
		}

		return 0, "", false
	})

	_, err := page.EnsureFolderAncestry(false, api, "DOCS", []string{"Manuals"}, &root.ID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `error creating folder with title "Manuals"`)
	assert.Contains(t, err.Error(), "403", "the reason the create was refused has to survive")
}

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
