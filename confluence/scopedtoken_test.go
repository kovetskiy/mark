package confluence_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cover issue #917: a scoped API token reaches Confluence through the
// api.atlassian.com gateway, which checks the token's granular scopes against
// the endpoint being called and refuses every v1 one to a token minted for
// pages. FindHomePage already fell back to v2 (#341), so a run got past the
// space lookup and then died on the first page lookup.

// scopedTokenGateway refuses v1 the way the gateway refuses it: with the status
// and body Atlassian answers a scope check with.
func scopedTokenGateway() confluencetest.FailFunc {
	return func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		return 0, "", false
	}
}

func TestFindPageFallsBackToV2(t *testing.T) {
	api, server := newAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, child.ID, page.ID)
	assert.Equal(t, "Child", page.Title)
	assert.Equal(t, "page", page.Type)
	assert.Equal(t, int64(1), page.Version.Number)

	// The ancestor chain is what mark decides page placement from, and v2 names
	// only the immediate parent -- so it has to be walked back rather than left
	// empty, which would read as a page sitting at the root of the space.
	require.Len(t, page.Ancestors, 1)
	assert.Equal(t, parent.ID, page.Ancestors[0].ID)
	assert.Equal(t, "Parent", page.Ancestors[0].Title)
}

// TestFindPageV2AncestorsAreRootFirst pins the order every caller relies on:
// the parent is the last entry, not the first.
func TestFindPageV2AncestorsAreRootFirst(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	middle := server.AddPage("DOCS", "Middle", "page", root.ID)
	server.AddPage("DOCS", "Leaf", "page", middle.ID)
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Leaf", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	require.Len(t, page.Ancestors, 2)
	assert.Equal(t, "Root", page.Ancestors[0].Title)
	assert.Equal(t, "Middle", page.Ancestors[1].Title)
}

// TestFindPageV2StopsAtAFolder: a folder is not an ancestor, and asking the
// page collection for one answers 404. The chain has to end there rather than
// fail the lookup.
func TestFindPageV2StopsAtAFolder(t *testing.T) {
	api, server := newAPI(t)
	folder := server.AddFolder("DOCS", "Folder", "", "space")
	server.AddPage("DOCS", "Inside", "page", folder.ID)
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Inside", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Empty(t, page.Ancestors)
}

// TestFindPageV2ReportsAbsence: the fallback has to be able to say "no such
// page" as plainly as v1 does, or every lookup of a page mark is about to
// create becomes an error.
func TestFindPageV2ReportsAbsence(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, page)
}

// TestFindPagePrefersV1 pins that the fallback is a fallback: a classic token
// gets its answer from v1 with no v2 round trip at all.
func TestFindPagePrefersV1(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Child", "page", "")

	page, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/pages"),
		"v1 answered, so v2 must not be consulted")
}

// TestFindPageReportsV1ErrorWhenV2CannotAnswer: on Server and Data Center there
// is no v2 at all, so a genuine 401 from v1 must survive the attempt rather
// than being replaced by a 404 from a path that never existed.
func TestFindPageReportsV1ErrorWhenV2Unavailable(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api") {
			return http.StatusUnauthorized, `{"message":"bad credentials"}`, true
		}
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			return http.StatusNotFound, `<html>404</html>`, true
		}
		return 0, "", false
	})

	_, err := api.FindPage("DOCS", "Child", "page")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestGetPageByIDFallsBackToV2(t *testing.T) {
	api, server := newAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	server.EditPage(child.ID, "<p>stored</p>")
	server.SetFail(scopedTokenGateway())

	page, err := api.GetPageByIDExpanded(child.ID, "ancestors,version,body.storage")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Child", page.Title)
	assert.Equal(t, "<p>stored</p>", page.Body.Storage.Value)
	require.Len(t, page.Ancestors, 1)
	assert.Equal(t, parent.ID, page.Ancestors[0].ID)
}

// TestGetPageByIDV2SkipsWhatWasNotExpanded: v2 charges a request per ancestor
// level, so a caller that asked for neither the body nor the ancestors must not
// pay for them.
func TestGetPageByIDV2SkipsWhatWasNotExpanded(t *testing.T) {
	api, server := newAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	server.SetFail(scopedTokenGateway())

	page, err := api.GetPageByIDExpanded(child.ID, "version")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Empty(t, page.Ancestors)

	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/pages/"),
		"the page itself, and nothing above it")
}

func TestCreatePageFallsBackToV2(t *testing.T) {
	api, server := newAPI(t)
	parentPage := server.AddPage("DOCS", "Parent", "page", "")
	server.SetFail(scopedTokenGateway())

	parent, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, parent)

	page, err := api.CreatePage("DOCS", "page", parent, "Created", "<p>new</p>")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Created", page.Title)

	stored := server.Page(page.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "<p>new</p>", stored.Body)
	assert.Equal(t, parentPage.ID, stored.ParentID)
	assert.Equal(t, int64(1), page.Version.Number,
		"the created version has to come back, or the first update is stale")

	// The ancestors of a created page are the parent's plus the parent, as they
	// are on v1: the caller updates the page a moment later and moves it to the
	// root of the space if it thinks it has none.
	require.Len(t, page.Ancestors, 1)
	assert.Equal(t, parentPage.ID, page.Ancestors[0].ID)
}

func TestUpdatePageFallsBackToV2(t *testing.T) {
	api, server := newAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	require.NoError(t, api.UpdatePage(page, "<p>published</p>", false, "a message", "full-width", ""))

	stored := server.Page(child.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "<p>published</p>", stored.Body)
	assert.Equal(t, int64(2), stored.Version)
	assert.Equal(t, "a message", stored.Message)
	assert.Equal(t, parent.ID, stored.ParentID, "an update must not move the page")
	assert.Equal(t, int64(2), page.Version.Number, "the caller's copy has to move with it")
}

// TestUpdatePageV2WritesTheProperties: v1 carries content appearance and the
// emoji title inside the update itself, and v2 has no room for them there. They
// are page properties, and dropping them would silently ignore
// --content-appearance and the emoji metadata for every scoped-token run.
func TestUpdatePageV2WritesTheProperties(t *testing.T) {
	api, server := newAPI(t)
	child := server.AddPage("DOCS", "Child", "page", "")
	server.SetFail(scopedTokenGateway())

	page, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	require.NoError(t, api.UpdatePage(page, "<p>published</p>", false, "", "full-width", "🐧"))

	appearance := server.SpaceProperty(child.ID, "content-appearance-published")
	require.NotNil(t, appearance)
	assert.JSONEq(t, `"full-width"`, string(appearance.Value))

	emoji := server.SpaceProperty(child.ID, "emoji-title-published")
	require.NotNil(t, emoji)
	assert.JSONEq(t, `"1f427"`, string(emoji.Value))
}

// TestUpdatePageV2UpdatesAPropertyItAlreadySet: the second run of the same
// document rewrites a property that exists, which v2 versions -- a create where
// an update was called for is a conflict, not an overwrite.
func TestUpdatePageV2UpdatesAPropertyItAlreadySet(t *testing.T) {
	api, server := newAPI(t)
	child := server.AddPage("DOCS", "Child", "page", "")
	server.SetFail(scopedTokenGateway())

	for _, appearance := range []string{"full-width", "default"} {
		page, err := api.GetPageByID(child.ID)
		require.NoError(t, err)
		require.NoError(t, api.UpdatePage(page, "<p>published</p>", false, "", appearance, ""))
	}

	property := server.SpaceProperty(child.ID, "content-appearance-published")
	require.NotNil(t, property)
	assert.JSONEq(t, `"default"`, string(property.Value))
}

// TestUpdatePageV2ReportsAPropertyFailureAfterPublishing: the content is in by
// the time the properties are written, so the failure has to say so -- and the
// version the run just wrote has to be recorded, or the next attempt collides
// with it.
func TestUpdatePageV2ReportsAPropertyFailureAfterPublishing(t *testing.T) {
	api, server := newAPI(t)
	child := server.AddPage("DOCS", "Child", "page", "")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api") {
			return http.StatusUnauthorized, `{"message":"scope does not match"}`, true
		}
		if strings.Contains(r.URL.Path, "/properties") {
			return http.StatusForbidden, `{"message":"no property scope"}`, true
		}
		return 0, "", false
	})

	page, err := api.GetPageByID(child.ID)
	require.NoError(t, err)

	err = api.UpdatePage(page, "<p>published</p>", false, "", "full-width", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "was updated")
	assert.Equal(t, int64(2), server.Page(child.ID).Version)
	assert.Equal(t, int64(2), page.Version.Number)
}

// TestPublishThroughTheGateway walks the sequence a scoped-token run makes:
// resolve the space, look the page up, create it, and update it. Each step used
// to end the run at the first v1 call it reached.
func TestPublishThroughTheGateway(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(scopedTokenGateway())

	root, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	require.NotNil(t, root)

	missing, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.Nil(t, missing)

	created, err := api.CreatePage("DOCS", "page", root, "Guide", "")
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(created, "<p>guide</p>", false, "", "full-width", ""))

	stored := server.Page(created.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "<p>guide</p>", stored.Body)
	assert.Equal(t, home.ID, stored.ParentID)
}

func newScopedAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()
	api, server := newAPI(t)
	server.SetFail(scopedTokenGateway())
	return api, server
}

// TestFindPageV2LooksUpTheSpaceOnce: v2 filters by space id where v1 takes a
// key, and a lookup per file would be a request per file.
func TestFindPageV2LooksUpTheSpaceOnce(t *testing.T) {
	api, server := newScopedAPI(t)
	server.AddPage("DOCS", "One", "page", "")
	server.AddPage("DOCS", "Two", "page", "")

	for _, title := range []string{"One", "Two"} {
		page, err := api.FindPage("DOCS", title, "page")
		require.NoError(t, err)
		require.NotNil(t, page)
	}

	assert.Equal(t, 1, server.CountRequestsMatching("GET", "/api/v2/spaces", "keys=DOCS"),
		"the space id is cached for the life of the API value")
}

// TestGetAttachmentsFallsBackToV2: the attachment listing is made for every
// page mark publishes, whether the document has attachments or not, so a token
// that cannot read it cannot publish at all. The checksum mark recognises an
// unchanged attachment by lives in the comment, which v2 keeps at the top level
// rather than under metadata.
func TestGetAttachmentsFallsBackToV2(t *testing.T) {
	api, server := newScopedAPI(t)
	page := server.AddPage("DOCS", "Child", "page", "")
	server.AddAttachment(page.ID, "diagram.png", "mark:checksum: abc123")

	attachments, err := api.GetAttachments(page.ID)
	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, "diagram.png", attachments[0].Filename)
	assert.Equal(t, "mark:checksum: abc123", attachments[0].Metadata.Comment)
	assert.Contains(t, attachments[0].Links.Download, "diagram.png")
	assert.Equal(t, "/wiki", attachments[0].Links.Context,
		"v2 gives no link context, and Cloud -- the only place v2 exists -- is mounted at /wiki")
}

// TestGetPageLabelsFallsBackToV2: labels are read for every page too, and v2
// hands the label id over as a number where v1 makes it a string.
func TestGetPageLabelsFallsBackToV2(t *testing.T) {
	api, server := newScopedAPI(t)
	stored := server.AddPage("DOCS", "Child", "page", "")
	server.AddLabel(stored.ID, "from-mark")

	page, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	labels, err := api.GetPageLabels(page, "global")
	require.NoError(t, err)
	require.Len(t, labels.Labels, 1)
	assert.Equal(t, "from-mark", labels.Labels[0].Name)
	assert.Equal(t, "global", labels.Labels[0].Prefix)
}

// dataCenter answers the way Confluence Server and Data Center do: v1 is the
// only API there is, and every /api/v2 path is a 404 from a route that was
// never going to exist.
func dataCenter(v1Status int, v1Path string) confluencetest.FailFunc {
	return func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			return http.StatusNotFound, `<html>404 Not Found</html>`, true
		}
		if v1Path != "" && strings.Contains(r.URL.Path, v1Path) {
			return v1Status, `{"message":"no"}`, true
		}
		return 0, "", false
	}
}

// TestDataCenterKeepsTheV1Failure is the reason the fallback is gated rather
// than merely attempted. A 403 and a 404 mean different things, and mark acts
// on the difference: page/orphan reads ErrNotFound as "the page is gone", which
// --on-orphan archives or trashes. Pairing v1's 403 with the 404 that any
// /api/v2 path answers on Data Center made a permission failure say exactly
// that.
func TestDataCenterKeepsTheV1Failure(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Doc", "page", "")
	server.SetFail(dataCenter(http.StatusForbidden, "/rest/api/content/"+page.ID))

	_, err := api.GetPageByID(page.ID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, confluence.ErrNotFound,
		"a page mark may not read is not a page that is gone")
	assert.Contains(t, err.Error(), "403")
	assert.NotContains(t, err.Error(), "/api/v2",
		"a deployment without v2 should not be told about a path it does not have")
}

// TestDataCenterProbesForV2Once: the probe that settles whether a deployment
// has a v2 API at all is made once for the life of the API value, however many
// calls are refused afterwards.
func TestDataCenterProbesForV2Once(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Doc", "page", "")
	server.SetFail(dataCenter(http.StatusForbidden, "/rest/api/content/"))

	for range 5 {
		_, err := api.GetPageByID(page.ID)
		require.Error(t, err)
	}

	assert.Equal(t, 1, server.CountRequestsMatching("GET", "/api/v2/spaces", "limit=1"),
		"one probe decides it, and the answer is remembered")
	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/pages"),
		"and no fallback is attempted against an API that is not there")
}

// TestDataCenterLookupStillReportsAbsence: the ordinary v1 answers have to go on
// meaning what they meant, gate or no gate.
func TestDataCenterLookupStillReportsAbsence(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(dataCenter(0, ""))

	page, err := api.FindPage("DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, page)

	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/pages"),
		"v1 answered, and there is no v2 to ask anyway")
}

// TestDataCenterPublishes walks a whole publish on a deployment that has no v2:
// the fallbacks must be invisible there.
func TestDataCenterPublishes(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(dataCenter(0, ""))

	root, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	require.NotNil(t, root)

	created, err := api.CreatePage("DOCS", "page", root, "Guide", "")
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(created, "<p>guide</p>", false, "", "full-width", ""))

	stored := server.Page(created.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "<p>guide</p>", stored.Body)
	assert.Equal(t, home.ID, stored.ParentID)
}
