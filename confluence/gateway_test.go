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
// api.atlassian.com gateway, which refuses every v1 endpoint to a token minted
// with the page scopes. Through the gateway, the page calls go to v2.

// newGatewayAPI stands in for the gateway: the base URL has the gateway's
// shape, and every v1 request is refused the way the gateway refuses one to a
// scoped token, so a call that still relies on v1 fails the test.
func newGatewayAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/rest/api/") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		return 0, "", false
	})
	return confluence.NewAPI(server.URL+"/ex/confluence/cloud-id", "user", "token", false), server
}

func TestGatewayFindPage(t *testing.T) {
	api, server := newGatewayAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	middle := server.AddPage("DOCS", "Middle", "page", root.ID)
	leaf := server.AddPage("DOCS", "Leaf", "page", middle.ID)

	page, err := api.FindPage("DOCS", "Leaf", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, leaf.ID, page.ID)
	assert.Equal(t, "Leaf", page.Title)
	assert.Equal(t, "page", page.Type)
	assert.Equal(t, int64(1), page.Version.Number)

	// v2 names only the immediate parent; mark places pages by the whole
	// chain, root first, with the parent last.
	require.Len(t, page.Ancestors, 2)
	assert.Equal(t, root.ID, page.Ancestors[0].ID)
	assert.Equal(t, "Root", page.Ancestors[0].Title)
	assert.Equal(t, middle.ID, page.Ancestors[1].ID)
	assert.Equal(t, "Middle", page.Ancestors[1].Title)

	assert.Equal(t, 0, server.CountRequests("GET", "/rest/api/content"),
		"through the gateway v1 is not asked at all")
}

// A folder is not an ancestor, so the chain ends there rather than failing
// the lookup -- the same rule the v1 path lives by.
func TestGatewayFindPageStopsAtAFolder(t *testing.T) {
	api, server := newGatewayAPI(t)
	folder := server.AddFolder("DOCS", "Folder", "", "space")
	server.AddPage("DOCS", "Inside", "page", folder.ID)

	page, err := api.FindPage("DOCS", "Inside", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Empty(t, page.Ancestors)
}

func TestGatewayFindPageReportsAbsence(t *testing.T) {
	api, server := newGatewayAPI(t)
	server.AddSpace("DOCS")

	page, err := api.FindPage("DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, page)
}

func TestGatewayGetPageByID(t *testing.T) {
	api, server := newGatewayAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	child := server.AddPage("DOCS", "Child", "page", parent.ID)
	server.EditPage(child.ID, "<p>stored</p>")

	page, err := api.GetPageByIDExpanded(child.ID, "ancestors,version,body.storage")
	require.NoError(t, err)
	assert.Equal(t, "Child", page.Title)
	assert.Equal(t, "<p>stored</p>", page.Body.Storage.Value)
	require.Len(t, page.Ancestors, 1)
	assert.Equal(t, parent.ID, page.Ancestors[0].ID)

	// Without ancestors in the expand list, no parent is read.
	server.ResetRequests()
	page, err = api.GetPageByIDExpanded(child.ID, "version")
	require.NoError(t, err)
	assert.Empty(t, page.Ancestors)
	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/pages/"+parent.ID))
}

func TestGatewayCreateAndUpdatePage(t *testing.T) {
	api, server := newGatewayAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")

	page, err := api.CreatePage("DOCS", "page", &confluence.PageInfo{ID: parent.ID, Title: "Parent"}, "Child", "<p>one</p>")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, int64(1), page.Version.Number)
	require.Len(t, page.Ancestors, 1)

	stored := server.Page(page.ID)
	require.NotNil(t, stored)
	assert.Equal(t, parent.ID, stored.ParentID)
	assert.Equal(t, "<p>one</p>", stored.Body)

	require.NoError(t, api.UpdatePage(page, "<p>two</p>", false, "second", "full-width", "🚀"))
	assert.Equal(t, int64(2), page.Version.Number)

	stored = server.Page(page.ID)
	assert.Equal(t, int64(2), stored.Version)
	assert.Equal(t, "<p>two</p>", stored.Body)
	assert.Equal(t, parent.ID, stored.ParentID, "an update must not move the page")

	// v1 carries these inside the update; v2 keeps them as page properties.
	appearance := server.SpaceProperty(page.ID, "content-appearance-published")
	require.NotNil(t, appearance)
	assert.JSONEq(t, `"full-width"`, string(appearance.Value))
	emoji := server.SpaceProperty(page.ID, "emoji-title-published")
	require.NotNil(t, emoji)
	assert.JSONEq(t, `"1f680"`, string(emoji.Value))

	// A second update supersedes the properties rather than colliding with them.
	require.NoError(t, api.UpdatePage(page, "<p>three</p>", false, "third", "fixed-width", ""))
	appearance = server.SpaceProperty(page.ID, "content-appearance-published")
	require.NotNil(t, appearance)
	assert.JSONEq(t, `"fixed-width"`, string(appearance.Value))
	assert.Equal(t, 2, appearance.Version)
}

func TestGatewayAttachmentsAndLabels(t *testing.T) {
	api, server := newGatewayAPI(t)
	page := server.AddPage("DOCS", "Doc", "page", "")
	server.AddAttachment(page.ID, "diagram.png", "[mark] abc123")
	server.AddLabel(page.ID, "from-mark")

	attachments, err := api.GetAttachments(page.ID)
	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, "diagram.png", attachments[0].Filename)
	assert.Equal(t, "[mark] abc123", attachments[0].Metadata.Comment)
	assert.NotEmpty(t, attachments[0].Links.Download)

	labels, err := api.GetPageLabels(&confluence.PageInfo{ID: page.ID}, "global")
	require.NoError(t, err)
	require.Len(t, labels.Labels, 1)
	assert.Equal(t, "from-mark", labels.Labels[0].Name)
}

// Outside the gateway nothing changes: v1 answers, and v2 is never asked about
// a page.
func TestFindPageOutsideTheGatewayStaysOnV1(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Doc", "page", "")

	page, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/content"))
	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/pages"))
}

// TestGatewayLinksUseTheSiteURL is #1019 on the v2 path: the configured base
// URL is the gateway, an API host a browser cannot open, so a page's base link
// has to be the site URL Confluence names in _links.base -- for a page found,
// one created in the same run, and one read by id.
func TestGatewayLinksUseTheSiteURL(t *testing.T) {
	const site = "https://tenant.atlassian.net/wiki"
	api, server := newGatewayAPI(t)
	server.SiteBase = site
	home := server.AddPage("DOCS", "Home", "page", "")

	found, err := api.FindPage("DOCS", "Home", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, site, found.Links.Base)

	created, err := api.CreatePage("DOCS", "page", found, "New", "<p/>")
	require.NoError(t, err)
	assert.Equal(t, site, created.Links.Base)

	cached, err := api.FindPage("DOCS", "New", "page")
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Equal(t, site, cached.Links.Base, "the cache populated by CreatePage keeps the site URL")

	byID, err := api.GetPageByID(home.ID)
	require.NoError(t, err)
	assert.Equal(t, site, byID.Links.Base)
}

// Without a site URL in the responses, the configured one stands, as on v1.
func TestGatewayLinksFallBackToTheBaseURL(t *testing.T) {
	api, server := newGatewayAPI(t)
	server.AddPage("DOCS", "Home", "page", "")

	found, err := api.FindPage("DOCS", "Home", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, api.BaseURL, found.Links.Base)
}
