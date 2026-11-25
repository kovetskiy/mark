package confluence_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refuseContentListing answers the /content listing 403, as an instance does
// that has disabled it, and leaves /content/search and single pages alone.
func refuseContentListing(server *confluencetest.Server) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/rest/api/content") {
			return http.StatusForbidden, `{"message":"refused"}`, true
		}
		return 0, "", false
	})
}

func contentGets(server *confluencetest.Server, suffix string) int {
	n := 0
	for _, req := range server.Requests() {
		if req.Method == http.MethodGet && strings.HasSuffix(strings.TrimRight(req.Path, "/"), suffix) {
			n++
		}
	}
	return n
}

func TestFindPageSearchesWhenContentListingIsRefused(t *testing.T) {
	api, server := newAPI(t)
	want := server.AddPage("DOCS", `A "quoted" title`, "page", "")
	server.AddPage("DOCS", "Other", "page", "")
	refuseContentListing(server)

	page, err := api.FindPage(context.Background(), "DOCS", `A "quoted" title`, "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, want.ID, page.ID)
	assert.NotZero(t, page.Version.Number)

	// The refusal is remembered: the next title goes straight to the search.
	missing, err := api.FindPage(context.Background(), "DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, missing)
	assert.Equal(t, 1, contentGets(server, "/rest/api/content"))
	assert.Equal(t, 2, contentGets(server, "/rest/api/content/search"))
}

func refuseSearch(server *confluencetest.Server, refuseListing bool) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		path := strings.TrimRight(r.URL.Path, "/")
		if r.Method == http.MethodGet &&
			((refuseListing && strings.HasSuffix(path, "/rest/api/content")) || strings.HasSuffix(path, "/content/search")) {
			return http.StatusForbidden, `{"message":"refused"}`, true
		}
		return 0, "", false
	})
}

func TestFindPageReportsTheRefusalWhenSearchFailsToo(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Page", "page", "")
	refuseSearch(server, true)

	_, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.ErrorIs(t, err, confluence.ErrForbidden)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "search fallback")
}

func TestFindPageDoesNotReadAnEmptySearchAsNoPageWhenTheListingIsRefused(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("SECRET")
	server.AddPage("DOCS", "Other", "page", "")
	refuseContentListing(server)

	// A 403 may mean the space may not be seen, and the search then finds
	// nothing: that must not read as a missing page, or the caller creates it.
	page, err := api.FindPage(context.Background(), "SECRET", "Page", "page")
	require.ErrorIs(t, err, confluence.ErrForbidden)
	assert.Nil(t, page)
	assert.Contains(t, err.Error(), "search fallback")

	for _, req := range server.Requests() {
		assert.NotEqual(t, http.MethodPost, req.Method, "nothing may be created: %s %s", req.Method, req.Path)
	}

	// The switch stayed unset, so the next lookup asks /content again.
	_, err = api.FindPage(context.Background(), "SECRET", "Page", "page")
	require.ErrorIs(t, err, confluence.ErrForbidden)
	assert.Equal(t, 2, contentGets(server, "/rest/api/content"))
}

func TestFindPageRetriesTheListingAfterAFailedSearch(t *testing.T) {
	api, server := newAPI(t)
	want := server.AddPage("DOCS", "Page", "page", "")
	refuseSearch(server, true)

	_, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.Error(t, err)

	// The listing comes back, and the search was never what the run settled on.
	server.SetFail(nil)
	server.ResetRequests()
	page, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, want.ID, page.ID)
	assert.Equal(t, 1, contentGets(server, "/rest/api/content"))
	assert.Equal(t, 0, contentGets(server, "/rest/api/content/search"))
}

func TestFindPageSearchesBlogpostsCaseInsensitivelyAndWithBackslashes(t *testing.T) {
	api, server := newAPI(t)
	blog := server.AddPage("DOCS", "Release Notes", "blogpost", "")
	server.AddPage("DOCS", "Release Notes", "page", "")
	backslash := server.AddPage("DOCS", `C:	emp\"x"`, "page", "")
	refuseContentListing(server)
	ctx := context.Background()

	found, err := api.FindPage(ctx, "DOCS", "release notes", "blogpost")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, blog.ID, found.ID)
	assert.Equal(t, confluence.ContentBlogPost, found.Type)

	found, err = api.FindPage(ctx, "DOCS", `C:	emp\"x"`, "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, backslash.ID, found.ID)
}

func TestFindPageSearchKeepsAncestors(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	mid := server.AddPage("DOCS", "Mid", "page", root.ID)
	leaf := server.AddPage("DOCS", "Leaf", "page", mid.ID)
	refuseContentListing(server)

	found, err := api.FindPage(context.Background(), "DOCS", "Leaf", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, leaf.ID, found.ID)
	require.Len(t, found.Ancestors, 2)
	assert.Equal(t, root.ID, found.Ancestors[0].ID)
	assert.Equal(t, mid.ID, found.Ancestors[1].ID)
}

func TestFindPageWithoutTitleSearchesTheSpace(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	server.AddPage("DOCS", "Child", "page", root.ID)
	refuseContentListing(server)

	found, err := api.FindPage(context.Background(), "DOCS", "", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, root.ID, found.ID)
}

// Cloud through the gateway looks pages up with v2 and never reaches the
// fallback, whatever /content would have answered.
func TestFindPageThroughTheGatewayNeverSearches(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL+"/ex/confluence/cloud-id", confluence.Options{Username: "user", Password: "token"})
	want := server.AddPage("DOCS", "Page", "page", "")
	refuseContentListing(server)

	found, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, want.ID, found.ID)
	assert.Equal(t, 0, contentGets(server, "/rest/api/content"))
	assert.Equal(t, 0, contentGets(server, "/content/search"))
}

func TestFindPageDoesNotReadAFailedSearchAsNoPageWhenTheListingIsRefused(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Page", "page", "")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		path := strings.TrimRight(r.URL.Path, "/")
		switch {
		case r.Method != http.MethodGet:
		case strings.HasSuffix(path, "/rest/api/content"):
			return http.StatusForbidden, `{"message":"refused"}`, true
		case strings.HasSuffix(path, "/content/search"):
			return http.StatusNotFound, `{"message":"no search here"}`, true
		}
		return 0, "", false
	})

	page, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.ErrorIs(t, err, confluence.ErrForbidden)
	assert.NotErrorIs(t, err, confluence.ErrNotFound)
	assert.Nil(t, page)
	assert.Contains(t, err.Error(), "404")

	for _, req := range server.Requests() {
		assert.NotEqual(t, http.MethodPost, req.Method, "nothing may be created: %s %s", req.Method, req.Path)
	}
}
